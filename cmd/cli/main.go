// Command ledger-cli is a gRPC client of a running ledger server. It keeps no
// datastore of its own: every command dials the server and calls the same RPCs
// as any other service caller, so it goes through the ledger's own validation
// and its scope checks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
)

const usage = `usage: ledger-cli <command> [flags] [arguments]

commands:
  post NOTE POSTING POSTING...  record a Transaction, one POSTING per leg
  balance [TYPE]                print the Trial balance, or one account type's balances
  register TYPE:USER:NAME       print an Account's Register in date order

A POSTING is TYPE:USER:NAME:AMOUNT+CURRENCY, four exact parts, as in
ASSETS:alice:Checking:-10.50+USD. TYPE is one of Assets, Liabilities, Equities,
Incomes, Expenses, matched case-insensitively. USER and NAME are opaque and
matched exactly: there is no hierarchy and no wildcard.

flags on every command:
  -addr    address of the ledger server (env LEDGER_ADDR, default localhost:8080)
  -token   service token presented to the ledger (env LEDGER_TOKEN, no default)

flags on post:
  -idempotency-key   key for the Transaction, generated when it is omitted

flags on balance:
  -user, -name, -currency   filter the balances on an exact value

flags on register:
  -currency   only Postings in this currency
  -reverse    newest Transaction date first
`

// accountTypes is the closed set an account type is matched against.
const accountTypes = "Assets, Liabilities, Equities, Incomes, Expenses"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", envOr("LEDGER_ADDR", "localhost:8080"), "address of the ledger server")
	token := fs.String("token", os.Getenv("LEDGER_TOKEN"), "service token presented to the ledger")
	parse := func() error {
		err := fs.Parse(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return errors.New(usage)
		}
		return err
	}

	// A dial or transport failure is not a ledger rejection, and is not worded
	// like one: the ledger answering "no" carries its own status code, while an
	// unreachable server only ever comes back as Unavailable.
	fail := func(err error) error {
		reported := status.Convert(err)
		if reported.Code() == codes.Unavailable {
			return fmt.Errorf("cannot reach the ledger at %s: %s", *addr, reported.Message())
		}
		return fmt.Errorf("the ledger rejected the request: %s [%s]", reported.Message(), reported.Code())
	}

	var call func(context.Context, pb.LedgerServiceClient) error

	switch args[0] {
	case "post":
		key := fs.String("idempotency-key", "", "idempotency key for the Transaction, generated when omitted")
		if err := parse(); err != nil {
			return err
		}
		rest := fs.Args()
		if len(rest) < 3 {
			return errors.New("post takes a note and two or more postings")
		}
		request := &pb.RecordTransactionRequest{IdempotencyKey: *key, Note: rest[0]}
		if request.IdempotencyKey == "" {
			request.IdempotencyKey = uuid.NewString()
		}
		for _, arg := range rest[1:] {
			posting, err := parsePosting(arg)
			if err != nil {
				return err
			}
			request.Postings = append(request.Postings, posting)
		}

		call = func(ctx context.Context, client pb.LedgerServiceClient) error {
			response, err := client.RecordTransaction(ctx, request)
			if status.Code(err) == codes.AlreadyExists {
				return fmt.Errorf("idempotency key %q was already recorded with different content, so nothing was recorded", request.IdempotencyKey)
			}
			if err != nil {
				return fail(err)
			}
			fmt.Print(postReport(request.IdempotencyKey, response))
			return nil
		}

	case "balance":
		user := fs.String("user", "", "only Accounts with exactly this user")
		name := fs.String("name", "", "only Accounts with exactly this name")
		currency := fs.String("currency", "", "only balances in this currency")
		if err := parse(); err != nil {
			return err
		}
		// The argument is an account type, not a prefix: names are flat, so
		// there is nothing to roll up. See docs/adr/0002-flat-account-names.md.
		filter := &pb.AccountFilter{}
		switch rest := fs.Args(); len(rest) {
		case 0:
		case 1:
			filter.Type = accountfmt.StringToAccountType(rest[0])
			if filter.Type == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
				return fmt.Errorf("unknown account type %q, expected one of %s", rest[0], accountTypes)
			}
		default:
			return errors.New("balance takes at most one account type")
		}
		// A flag left off filters on nothing, while -user "" asks for the
		// Accounts whose user really is empty.
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "user":
				filter.User = user
			case "name":
				filter.Name = name
			}
		})

		call = func(ctx context.Context, client pb.LedgerServiceClient) error {
			response, err := client.ListAccountBalances(ctx, &pb.ListAccountBalancesRequest{
				Account:      filter,
				CurrencyCode: *currency,
			})
			if err != nil {
				return fail(err)
			}
			for _, balance := range response.Balances {
				fmt.Printf("%-32s %16s\n", formatAccount(balance.Account), formatMoney(balance.Balance))
			}
			return nil
		}

	case "register":
		currency := fs.String("currency", "", "only Postings in this currency")
		reverse := fs.Bool("reverse", false, "newest Transaction date first")
		if err := parse(); err != nil {
			return err
		}
		rest := fs.Args()
		if len(rest) != 1 {
			return errors.New("register takes exactly one account, as TYPE:USER:NAME")
		}
		account, err := parseAccount(rest[0])
		if err != nil {
			return err
		}
		request := &pb.ListPostingsRequest{
			Filter: &pb.PostingFilter{
				Account:      &pb.AccountFilter{Type: account.Type, User: &account.User, Name: &account.Name},
				CurrencyCode: *currency,
			},
			PageSize:         100,
			PageNumber:       1,
			OrderByAscending: !*reverse,
		}

		call = func(ctx context.Context, client pb.LedgerServiceClient) error {
			response, err := client.ListPostings(ctx, request)
			if err != nil {
				return fail(err)
			}
			for _, posting := range response.Postings {
				fmt.Printf("%-25s %16s  balance %16s  %s\n",
					formatDate(posting.Date), formatMoney(posting.Amount), formatMoney(posting.Balance), posting.TransactionId)
			}
			if int64(len(response.Postings)) < response.TotalCount {
				fmt.Printf("(showing %d of %d postings)\n", len(response.Postings), response.TotalCount)
			}
			return nil
		}

	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}

	if *token == "" {
		return errors.New("no token: pass -token or set LEDGER_TOKEN")
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("cannot reach the ledger at %s: %w", *addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "bearer "+*token)

	return call(ctx, pb.NewLedgerServiceClient(conn))
}

// postReport renders what `post` prints for a successful RecordTransaction. A
// replay is one of those: the same key with identical content recorded the
// money exactly once, which is what the caller wanted, so the original
// Transaction is printed exactly as a fresh record is and only the wording
// tells the two apart.
func postReport(key string, response *pb.RecordTransactionResponse) string {
	transaction := response.Transaction
	verb := "recorded"
	if response.Replayed {
		verb = "replayed"
	}

	var report strings.Builder
	fmt.Fprintf(&report, "%s %s  %s  %s\n", verb, transaction.Id, formatDate(transaction.Date), transaction.Note)
	for _, posting := range transaction.Postings {
		fmt.Fprintf(&report, "  %-32s %16s  balance %s\n",
			formatAccount(posting.Account), formatMoney(posting.Amount), formatMoney(posting.Balance))
	}
	if response.Replayed {
		fmt.Fprintf(&report, "(idempotency key %q was already recorded, so nothing new was recorded)\n", key)
	}
	return report.String()
}

// parsePosting parses one posting argument, TYPE:USER:NAME:AMOUNT+CURRENCY.
// Every refusal names the argument that failed.
func parsePosting(arg string) (*pb.RecordTransactionRequest_PostingInput, error) {
	parts := strings.Split(arg, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("posting %q is not TYPE:USER:NAME:AMOUNT+CURRENCY", arg)
	}
	account, err := accountFrom(arg, parts)
	if err != nil {
		return nil, err
	}
	amount, currency, found := strings.Cut(parts[3], "+")
	if !found || currency == "" {
		return nil, fmt.Errorf("posting %q has no currency: the amount reads AMOUNT+CURRENCY, as in 10.50+USD", arg)
	}
	value, err := decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("posting %q has a malformed amount %q", arg, amount)
	}
	return &pb.RecordTransactionRequest_PostingInput{
		Account: account,
		Amount:  moneyfmt.FromDecimal(value, currency),
	}, nil
}

// parseAccount parses an account argument, TYPE:USER:NAME.
func parseAccount(arg string) (*pb.Account, error) {
	parts := strings.Split(arg, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("account %q is not TYPE:USER:NAME", arg)
	}
	return accountFrom(arg, parts)
}

// accountFrom reads an Account off the first three parts of an argument. The
// type is matched case-insensitively against the five categories; user and name
// are opaque and taken exactly as written.
func accountFrom(arg string, parts []string) (*pb.Account, error) {
	accountType := accountfmt.StringToAccountType(parts[0])
	if accountType == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("%q has unknown account type %q, expected one of %s", arg, parts[0], accountTypes)
	}
	return &pb.Account{Type: accountType, User: parts[1], Name: parts[2]}, nil
}

func formatAccount(account *pb.Account) string {
	if account == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", accountfmt.AccountTypeToString(account.Type), account.User, account.Name)
}

func formatMoney(amount *money.Money) string {
	value, err := moneyfmt.ToDecimal(amount)
	if err != nil {
		return "?"
	}
	return value.String() + " " + amount.CurrencyCode
}

func formatDate(date *timestamppb.Timestamp) string {
	if date == nil {
		return ""
	}
	return date.AsTime().Local().Format(time.RFC3339)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
