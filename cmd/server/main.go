package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/tryvium-travels/memongo"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	grpcprom "github.com/grpc-ecosystem/go-grpc-prometheus"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/middleware"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
	"github.com/squall-chua/go-ledger-microservice/internal/service"
)

func main() {
	port := flag.String("port", "8080", "port to listen on")
	dbType := flag.String("db-type", "sqlite", "Type of database to use: 'sqlite', 'postgres', 'mysql', 'sqlserver', or 'mongo'")
	sqlDSN := flag.String("sql-dsn", "ledger.db", "Data Source Name for SQL database")
	corsOrigins := flag.String("cors-origins", "*", "comma-separated list of allowed CORS origins")
	jwtSecret := flag.String("jwt-secret", "super-secret-key", "secret key for JWT validation")
	mongoURI := flag.String("mongo-uri", "", "MongoDB connection URI. If empty, starts an in-memory memongo instance.")
	flag.Parse()

	ctx := context.Background()

	// 1. Setup Data Store
	var repo repository.LedgerRepository
	var mongoServer *memongo.Server
	var mongoClient *mongo.Client

	if *dbType == "mongo" {
		var finalMongoURI string

		if *mongoURI == "" {
			log.Println("No mongo-uri provided, starting Memongo...")
			var err error
			mongoServer, err = memongo.Start("8.2.5")
			if err != nil {
				log.Fatalf("Failed to start Memongo: %v", err)
			}
			finalMongoURI = mongoServer.URI()
		} else {
			finalMongoURI = *mongoURI
		}

		clientOpts := options.Client().ApplyURI(finalMongoURI)
		var err error
		mongoClient, err = mongo.Connect(clientOpts)
		if err != nil {
			log.Fatalf("Failed to connect to Memongo: %v", err)
		}

		// Parse the connection URI to extract the database name
		cs, err := connstring.ParseAndValidate(finalMongoURI)
		if err != nil {
			log.Fatalf("Failed to parse MongoDB URI: %v", err)
		}

		dbName := cs.Database
		if dbName == "" {
			log.Panic("No database specified in MongoDB URI")
		}
		db := mongoClient.Database(dbName)
		repo = repository.NewMongoLedgerRepository(db)
	} else {
		log.Printf("Using %s database...", *dbType)
		var dialector gorm.Dialector
		switch *dbType {
		case "postgres":
			dialector = postgres.Open(*sqlDSN)
		case "mysql":
			dialector = mysql.Open(*sqlDSN)
		case "sqlserver":
			dialector = sqlserver.Open(*sqlDSN)
		case "sqlite", "":
			dialector = sqlite.Open(*sqlDSN)
		default:
			log.Fatalf("Unsupported SQL db-type: %s", *dbType)
		}

		db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			log.Fatalf("Failed to connect to %s: %v", *dbType, err)
		}
		repo = repository.NewSQLLedgerRepository(db)
	}

	if mongoClient != nil {
		defer mongoClient.Disconnect(ctx)
	}

	svc := service.NewLedgerService(repo)
	// 2. Setup gRPC Server with Interceptors
	validator := middleware.NewJwtTokenValidator(*jwtSecret)

	grpcMetrics := grpcprom.NewServerMetrics()

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpcMetrics.UnaryServerInterceptor(),
			middleware.AuthInterceptor(validator),
		),
		grpc.ChainStreamInterceptor(
			grpcMetrics.StreamServerInterceptor(),
		),
	)

	// Register Services
	pb.RegisterLedgerServiceServer(grpcServer, svc)
	grpcMetrics.InitializeMetrics(grpcServer)

	// Register Healthcheck
	healthcheck := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthcheck)
	healthcheck.SetServingStatus("LedgerService", grpc_health_v1.HealthCheckResponse_SERVING)

	// 4. Setup REST Gateway & HTTP Multiplexer
	gwmux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			if strings.ToLower(key) == "authorization" {
				return "authorization", true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
	)

	addr := fmt.Sprintf(":%s", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Dial using a custom dialer targeting the listener address
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return net.Dial("tcp", lis.Addr().String())
		}),
	}

	err = pb.RegisterLedgerServiceHandlerFromEndpoint(ctx, gwmux, lis.Addr().String(), dopts)
	if err != nil {
		log.Fatalf("Failed to register gateway: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", gwmux)

	originsList := strings.Split(*corsOrigins, ",")
	for i := range originsList {
		originsList[i] = strings.TrimSpace(originsList[i])
	}

	c := cors.New(cors.Options{
		AllowedOrigins:   originsList, // specify allowed origin
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"*"}, // allow all headers for simplicity, or specify Authorization, Content-Type, etc.
		AllowCredentials: true,
	})
	corsHandler := c.Handler(mux)

	log.Printf("Starting Multiplexed gRPC & HTTP server on %s\n", addr)

	// Use h2c so we can handle HTTP/2 without TLS
	mixedHandler := grpcHandlerFunc(grpcServer, corsHandler)
	h2cHandler := h2c.NewHandler(mixedHandler, &http2.Server{})

	httpServer := &http.Server{
		Handler: h2cHandler,
	}

	go func() {
		if err := httpServer.Serve(lis); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the servers
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down servers...")

	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(ctxShutdown); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	if mongoServer != nil {
		mongoServer.Stop()
	}

	log.Println("Servers gracefully stopped")
}

// grpcHandlerFunc separates gRPC requests from HTTP requests.
func grpcHandlerFunc(grpcServer *grpc.Server, otherHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// gRPC requests use HTTP/2 and have Content-Type "application/grpc"
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
		} else {
			otherHandler.ServeHTTP(w, r)
		}
	})
}
