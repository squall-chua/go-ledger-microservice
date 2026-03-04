package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/middleware"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
	"github.com/squall-chua/go-ledger-microservice/internal/service"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func getRepo() repository.LedgerRepository {
	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "sqlite"
	}

	if dbType == "mongo" {
		uri := os.Getenv("MONGO_URI")
		if uri == "" {
			uri = "mongodb://localhost:27017/ledger_db"
		}
		clientOpts := options.Client().ApplyURI(uri)
		client, err := mongo.Connect(clientOpts)
		if err != nil {
			log.Fatalf("mongo connect error: %v", err)
		}
		db := client.Database("ledger_db")
		return repository.NewMongoLedgerRepository(db)
	}

	dsn := os.Getenv("SQL_DSN")
	if dsn == "" {
		dsn = "ledger.db"
	}
	var dialector gorm.Dialector
	switch dbType {
	case "postgres":
		dialector = postgres.Open(dsn)
	case "mysql":
		dialector = mysql.Open(dsn)
	case "sqlserver":
		dialector = sqlserver.Open(dsn)
	case "sqlite", "":
		dialector = sqlite.Open(dsn)
	default:
		log.Fatalf("Unsupported SQL db-type: %s", dbType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("sql connect error: %v", err)
	}
	return repository.NewSQLLedgerRepository(db)
}

func main() {
	var rootCmd = &cobra.Command{Use: "ledger-cli"}

	getSvcAndCtx := func() (pb.LedgerServiceServer, context.Context) {
		repo := getRepo()
		svc := service.NewLedgerService(repo)
		ctx := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
			Roles: []string{"admin"},
		})
		return svc, ctx
	}

	var currencyCmd string
	var balanceCmd = &cobra.Command{
		Use:   "balance [account]",
		Short: "Get account balance",
		Run: func(cmd *cobra.Command, args []string) {
			svc, ctx := getSvcAndCtx()
			acc := "*"
			if len(args) > 0 {
				acc = args[0]
			}
			req := &pb.GetAccountBalanceRequest{
				Account:  accountfmt.ParseString(acc),
				Currency: currencyCmd,
			}
			res, err := svc.GetAccountBalance(ctx, req)
			if err != nil {
				log.Fatalf("Error: %v", err)
			}
			for _, b := range res.Balances {
				accStr := accountfmt.BuildString(b.Account)
				dec, _ := moneyfmt.ToDecimal(b.Balance)
				if b.UpdatedAt == nil {
					fmt.Printf("%-20s %s %s\n", accStr, dec.String(), b.Balance.CurrencyCode)
				} else {
					fmt.Printf("%-20s %s %s (Updated: %s)\n", accStr, dec.String(), b.Balance.CurrencyCode, b.UpdatedAt.AsTime().Local().Format("2006-01-02 15:04:05MST"))
				}
			}
		},
	}

	var descFlag bool
	var registerCmd = &cobra.Command{
		Use:   "register",
		Short: "List transactions",
		Run: func(cmd *cobra.Command, args []string) {
			svc, ctx := getSvcAndCtx()
			req := &pb.ListTransactionsRequest{
				PageSize:    50,
				PageNumber:  1,
				OrderByDesc: descFlag,
			}
			res, err := svc.ListTransactions(ctx, req)
			if err != nil {
				log.Fatalf("Error: %v", err)
			}
			for _, t := range res.Transactions {
				timeStr := ""
				if t.Date != nil {
					timeStr = t.Date.AsTime().Local().Format("2006-01-02 15:04:05MST")
				}
				fmt.Printf("%s - %s\n", timeStr, t.Note)
				for _, p := range t.Postings {
					dec, _ := moneyfmt.ToDecimal(p.Amount)
					balDec, _ := moneyfmt.ToDecimal(p.Balance)
					fmt.Printf("    %-20s %10s %s   (= %10s %s)\n", accountfmt.BuildString(p.Account), dec.String(), p.Amount.CurrencyCode, balDec.String(), p.Balance.CurrencyCode)
				}
			}
		},
	}

	var postCmd = &cobra.Command{
		Use:   "post [note] [account:amount]...",
		Short: "Record a new transaction",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			note := args[0]
			svc, ctx := getSvcAndCtx()

			req := &pb.RecordTransactionRequest{
				IdempotencyKey: uuid.New().String(),
				Date:           timestamppb.New(time.Now().UTC()),
				Note:           note,
			}

			for i := 1; i < len(args); i++ {
				lastColon := strings.LastIndex(args[i], ":")
				if lastColon == -1 {
					log.Fatalf("Invalid posting format: %s", args[i])
				}
				acc := args[i][:lastColon]
				amtStr := args[i][lastColon+1:]
				currency := "USD"
				if len(amtStr) > 3 {
					last3 := amtStr[len(amtStr)-3:]
					if strings.ToUpper(last3) == last3 && !strings.ContainsAny(last3, "0123456789.") {
						currency = last3
						amtStr = amtStr[:len(amtStr)-3]
					}
				}

				dec, err := decimal.NewFromString(amtStr)
				if err != nil {
					log.Fatalf("Invalid amount %s: %v", amtStr, err)
				}

				req.Postings = append(req.Postings, &pb.RecordTransactionRequest_PostingInput{
					Account: accountfmt.ParseString(acc),
					Amount:  moneyfmt.FromDecimal(dec, currency),
				})
			}

			_, err := svc.RecordTransaction(ctx, req)
			if err != nil {
				log.Fatalf("Error recording transaction: %v", err)
			}
			fmt.Println("Transaction recorded successfully.")
		},
	}

	balanceCmd.Flags().StringVarP(&currencyCmd, "currency", "c", "", "Filter balance by currency")
	registerCmd.Flags().BoolVarP(&descFlag, "desc", "d", false, "Sort descending (newest first)")

	rootCmd.AddCommand(balanceCmd, registerCmd, postCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
