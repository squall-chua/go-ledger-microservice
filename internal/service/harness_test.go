package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/middleware"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
)

// Every behavioural test in this package is a real RPC over an in-process
// connection, with the auth interceptor installed and a real embedded Postgres
// behind it: no Docker, no installed database. One cluster is started for the
// whole package; each test gets its own database inside it.

const testJWTSecret = "test-secret"

var (
	pgPort  uint32
	adminDB *sql.DB
)

func TestMain(m *testing.M) {
	postgres, dataDir, err := startPostgres()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	adminDB.Close()
	if err := postgres.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "stop embedded postgres:", err)
	}
	os.RemoveAll(dataDir)
	os.Exit(code)
}

// startPostgres starts one cluster for the package. The port is picked and
// released well before the cluster binds it — the binaries extract and initdb
// runs in between — so another process can take it in the gap; a retry with a
// fresh port makes losing that race cost a second rather than the suite.
func startPostgres() (*embeddedpostgres.EmbeddedPostgres, string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		port, err := freePort()
		if err != nil {
			return nil, "", err
		}
		dir, err := os.MkdirTemp("", "ledger-pg-")
		if err != nil {
			return nil, "", err
		}

		postgres := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Port(port).
			DataPath(filepath.Join(dir, "data")).
			RuntimePath(filepath.Join(dir, "run")).
			StartTimeout(60 * time.Second).
			Logger(io.Discard))

		if err := postgres.Start(); err != nil {
			lastErr = err
			os.RemoveAll(dir)
			continue
		}

		adminDB, err = sql.Open("pgx", dsn("postgres", port))
		if err != nil {
			postgres.Stop()
			os.RemoveAll(dir)
			return nil, "", err
		}
		pgPort = port
		return postgres, dir, nil
	}
	return nil, "", fmt.Errorf("start embedded postgres: %w", lastErr)
}

func freePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return uint32(listener.Addr().(*net.TCPAddr).Port), nil
}

func dsn(database string, port uint32) string {
	return fmt.Sprintf("postgres://postgres:postgres@localhost:%d/%s?sslmode=disable", port, database)
}

// newDatabase gives one test its own database with the schema applied.
func newDatabase(t *testing.T) *sql.DB {
	t.Helper()

	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	_, err := adminDB.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	t.Cleanup(func() {
		adminDB.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})

	db, err := sql.Open("pgx", dsn(name, pgPort))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	require.NoError(t, repository.ApplySchema(t.Context(), db))
	return db
}

// harness is a running ledger: an isolated database, the service behind the
// auth interceptor, and a client holding a valid service token.
type harness struct {
	t      *testing.T
	client pb.LedgerServiceClient
	ctx    context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	validator := middleware.NewJwtTokenValidator(testJWTSecret)
	server := grpc.NewServer(grpc.UnaryInterceptor(middleware.AuthInterceptor(validator)))
	pb.RegisterLedgerServiceServer(server, NewLedgerService(repository.New(newDatabase(t))))

	listener := bufconn.Listen(1024 * 1024)
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("bufconn server exited: %v", err)
		}
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return &harness{
		t:      t,
		client: pb.NewLedgerServiceClient(conn),
		ctx:    callerContext(t, t.Context(), "ledger:read", "ledger:write"),
	}
}

// callerContext carries a service token bearing the given scopes. The book has
// no tenant and no end user, so a scope is the whole of what a caller is.
func callerContext(t *testing.T, ctx context.Context, scopes ...string) context.Context {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"scope": strings.Join(scopes, " "),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	return bearerContext(ctx, token)
}

func bearerContext(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "bearer "+token)
}

func (h *harness) record(request *pb.RecordTransactionRequest) (*pb.RecordTransactionResponse, error) {
	h.t.Helper()
	return h.client.RecordTransaction(h.ctx, request)
}

func (h *harness) mustRecord(request *pb.RecordTransactionRequest) *pb.Transaction {
	h.t.Helper()
	response, err := h.record(request)
	require.NoError(h.t, err)
	return response.Transaction
}

func (h *harness) balances(request *pb.ListAccountBalancesRequest) []*pb.AccountBalance {
	h.t.Helper()
	response, err := h.client.ListAccountBalances(h.ctx, request)
	require.NoError(h.t, err)
	return response.Balances
}

func (h *harness) transactions(request *pb.ListTransactionsRequest) *pb.ListTransactionsResponse {
	h.t.Helper()
	response, err := h.client.ListTransactions(h.ctx, request)
	require.NoError(h.t, err)
	return response
}

// register reads one account's postings back through ListPostings.
func (h *harness) register(request *pb.ListPostingsRequest) *pb.ListPostingsResponse {
	h.t.Helper()
	response, err := h.client.ListPostings(h.ctx, request)
	require.NoError(h.t, err)
	return response
}

// transfer is the ordinary two-legged transaction the tests lean on: `amount`
// out of `from` and into `to`, in USD.
func transfer(key, note string, from, to *pb.Account, amount *money.Money) *pb.RecordTransactionRequest {
	return &pb.RecordTransactionRequest{
		IdempotencyKey: key,
		Note:           note,
		Postings: []*pb.RecordTransactionRequest_PostingInput{
			posting(to, amount),
			posting(from, negate(amount)),
		},
	}
}

func posting(account *pb.Account, amount *money.Money) *pb.RecordTransactionRequest_PostingInput {
	return &pb.RecordTransactionRequest_PostingInput{Account: account, Amount: amount}
}

func account(accountType pb.AccountType, user, name string) *pb.Account {
	return &pb.Account{Type: accountType, User: user, Name: name}
}

// exactly asks for one account and nothing else: every field is set, so an
// empty user or name is filtered for rather than ignored.
func exactly(a *pb.Account) *pb.AccountFilter {
	return &pb.AccountFilter{Type: a.Type, User: proto.String(a.User), Name: proto.String(a.Name)}
}

func amount(currencyCode string, units int64, nanos int32) *money.Money {
	return &money.Money{CurrencyCode: currencyCode, Units: units, Nanos: nanos}
}

func usd(units int64, nanos int32) *money.Money {
	return amount("USD", units, nanos)
}

func negate(m *money.Money) *money.Money {
	return amount(m.CurrencyCode, -m.Units, -m.Nanos)
}

func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, want, status.Code(err), "message was: %v", err)
}
