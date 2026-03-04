package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sqlLedgerRepo struct {
	db *gorm.DB
}

func NewSQLLedgerRepository(db *gorm.DB) LedgerRepository {
	db.AutoMigrate(&SQLTransaction{}, &SQLPosting{}, &SQLAccount{})
	return &sqlLedgerRepo{db: db}
}

type SQLTransaction struct {
	ID             string `gorm:"primaryKey"`
	IdempotencyKey string `gorm:"uniqueIndex"`
	Date           time.Time
	Note           string
	Metadata       datatypes.JSON // Storing Any as JSON
	Postings       []SQLPosting   `gorm:"foreignKey:TransactionID"`
	CreatedAt      time.Time
}

type SQLPosting struct {
	ID            string          `gorm:"primaryKey"`
	TransactionID string          `gorm:"index"`
	AccountName   string          `gorm:"index"`
	Amount        decimal.Decimal `gorm:"type:decimal(36,18)"`
	Balance       decimal.Decimal `gorm:"type:decimal(36,18)"`
	Currency      string
	CreatedAt     time.Time
}

type SQLAccount struct {
	Name      string          `gorm:"primaryKey"`
	Currency  string          `gorm:"primaryKey"`
	Balance   decimal.Decimal `gorm:"type:decimal(36,18)"`
	UpdatedAt time.Time
}

func (r *sqlLedgerRepo) RecordTransaction(ctx context.Context, txn *pb.Transaction, verifyNonNegativeBalances []string) error {
	// Check idempotency first outside transaction
	var existing SQLTransaction
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", txn.IdempotencyKey).First(&existing).Error
	if err == nil {
		return ErrIdempotentHit
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Verify Idempotency inside tx
		var innerExisting SQLTransaction
		err := tx.Where("idempotency_key = ?", txn.IdempotencyKey).First(&innerExisting).Error
		if err == nil {
			return ErrIdempotentHit
		}

		if txn.Id == "" {
			txn.Id = uuid.New().String()
		}
		if txn.CreatedAt == nil {
			txn.CreatedAt = timestamppb.New(time.Now().UTC())
		}

		var currency string
		for _, p := range txn.Postings {
			if currency == "" {
				currency = p.Amount.CurrencyCode
			} else if currency != p.Amount.CurrencyCode {
				return ErrCurrencyMismatch
			}
		}

		// Insert Postings and update Account Balances
		for _, p := range txn.Postings {
			if p.Id == "" {
				p.Id = uuid.New().String()
			}
			p.TransactionId = txn.Id

			amountDec, err := moneyfmt.ToDecimal(p.Amount)
			if err != nil {
				return err
			}

			// Upsert account running balance (composite key: name + currency)
			var acc SQLAccount
			accNameStr := accountfmt.BuildString(p.Account)
			err = tx.Where("name = ? AND currency = ?", accNameStr, p.Amount.CurrencyCode).First(&acc).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				acc = SQLAccount{
					Name:      accNameStr,
					Currency:  p.Amount.CurrencyCode,
					Balance:   amountDec,
					UpdatedAt: time.Now().UTC(),
				}
				if err := tx.Create(&acc).Error; err != nil {
					return err
				}
			} else if err == nil {
				acc.Balance = acc.Balance.Add(amountDec)
				acc.UpdatedAt = time.Now().UTC()
				if err := tx.Save(&acc).Error; err != nil {
					return err
				}
			} else {
				return err
			}

			// Set running balance into the protobuf POSTING struct
			dbBal := acc.Balance
			pbBal := moneyfmt.FromDecimal(dbBal, acc.Currency)
			p.Balance = &money.Money{
				CurrencyCode: pbBal.CurrencyCode,
				Units:        pbBal.Units,
				Nanos:        pbBal.Nanos,
			}

			mp := SQLPosting{
				ID:            p.Id,
				TransactionID: p.TransactionId,
				AccountName:   accNameStr,
				Amount:        amountDec,
				Balance:       dbBal,
				Currency:      p.Amount.CurrencyCode,
				CreatedAt:     txn.CreatedAt.AsTime(),
			}

			if err := tx.Create(&mp).Error; err != nil {
				return err
			}
		}

		// Save Transaction
		var metaJSON datatypes.JSON
		if len(txn.Metadata) > 0 {
			metaMap := make(map[string][]byte)
			for k, v := range txn.Metadata {
				b, _ := proto.Marshal(v)
				metaMap[k] = b
			}
			metaBytes, _ := json.Marshal(metaMap)
			metaJSON = datatypes.JSON(metaBytes)
		}

		mt := SQLTransaction{
			ID:             txn.Id,
			IdempotencyKey: txn.IdempotencyKey,
			Date:           txn.Date.AsTime(),
			Note:           txn.Note,
			Metadata:       metaJSON,
			CreatedAt:      txn.CreatedAt.AsTime(),
		}
		if err := tx.Create(&mt).Error; err != nil {
			return err
		}

		// Verify Non-Negative Balances using the updated balances inside the postings
		for _, vAccount := range verifyNonNegativeBalances {
			isWildcard := strings.Contains(vAccount, "*")

			for _, p := range txn.Postings {
				accNameStr := accountfmt.BuildString(p.Account)
				match := false
				if isWildcard {
					// Extremely simplistic in-memory LIKE matching is complex, so we fallback
					// to string prefixing if the pattern is simple X%. Otherwise string checking here in
					// memory for wildcard `*:User:*` needs an elaborate regex. To keep robust efficiency,
					// we just perform simple match if the only wildcard is at the end.
					// However, a complete memory Match requires regex mapping.
					// Let's implement robust regex matching:
					regexPattern := "^" + strings.ReplaceAll(vAccount, "*", ".*") + "$"
					match, _ = regexp.MatchString(regexPattern, accNameStr)
				} else {
					match = (accNameStr == vAccount)
				}

				if match && p.Balance != nil {
					balDec, _ := moneyfmt.ToDecimal(p.Balance)
					if balDec.IsNegative() {
						return fmt.Errorf("account %s has negative balance: %s %s", accNameStr, balDec.String(), p.Balance.CurrencyCode)
					}
				}
			}
		}

		return nil
	})
}

func (r *sqlLedgerRepo) GetAccountBalance(ctx context.Context, accountName string, currencyFilter string) ([]*pb.AccountBalance, error) {
	// If exact match
	if !strings.Contains(accountName, "*") {
		var accounts []SQLAccount
		q := r.db.WithContext(ctx).Where("name = ?", accountName)
		if currencyFilter != "" {
			q = q.Where("currency = ?", currencyFilter)
		}
		err := q.Find(&accounts).Error
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			return []*pb.AccountBalance{}, nil
		}

		var result []*pb.AccountBalance
		for _, acc := range accounts {
			pbBal := &pb.AccountBalance{
				Account: accountfmt.ParseString(acc.Name),
				Balance: moneyfmt.FromDecimal(acc.Balance, acc.Currency),
			}
			if !acc.UpdatedAt.IsZero() {
				pbBal.UpdatedAt = timestamppb.New(acc.UpdatedAt)
			}
			result = append(result, pbBal)
		}

		return result, nil
	}

	// If wildcard match e.g. Assets:*
	// Need to check if it's a wildcard match or not. `accountName` string might contain '*'.
	pattern := strings.ReplaceAll(accountName, "*", "%")
	var accounts []SQLAccount
	q := r.db.WithContext(ctx).Where("name LIKE ?", pattern)
	if currencyFilter != "" {
		q = q.Where("currency = ?", currencyFilter)
	}
	err := q.Find(&accounts).Error
	if err != nil {
		return nil, err
	}

	var result []*pb.AccountBalance
	for _, acc := range accounts {
		pbBal := &pb.AccountBalance{
			Account: accountfmt.ParseString(acc.Name),
			Balance: moneyfmt.FromDecimal(acc.Balance, acc.Currency),
		}
		if !acc.UpdatedAt.IsZero() {
			pbBal.UpdatedAt = timestamppb.New(acc.UpdatedAt)
		}
		result = append(result, pbBal)
	}

	return result, nil
}

func (r *sqlLedgerRepo) ListTransactions(ctx context.Context, filter *pb.TransactionFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Transaction, int64, error) {
	query := r.db.WithContext(ctx).Model(&SQLTransaction{})

	if filter != nil {
		if filter.StartDate != nil {
			query = query.Where("date >= ?", filter.StartDate.AsTime())
		}
		if filter.EndDate != nil {
			query = query.Where("date <= ?", filter.EndDate.AsTime())
		}

		if len(filter.Id) > 0 {
			query = query.Where("id IN ?", filter.Id)
		}
		if len(filter.IdempotencyKey) > 0 {
			query = query.Where("idempotency_key IN ?", filter.IdempotencyKey)
		}
		if filter.Note != "" {
			pattern := filter.Note
			if strings.Contains(pattern, "*") {
				pattern = strings.ReplaceAll(pattern, "*", "%")
				query = query.Where("note LIKE ?", pattern)
			} else {
				query = query.Where("note = ?", pattern)
			}
		}

		if len(filter.MetadataFilters) > 0 {
			for _, mf := range filter.MetadataFilters {
				var val interface{}
				if mf.Value != nil {
					valBytes, _ := proto.Marshal(mf.Value)
					val = valBytes
				}

				switch mf.Operator {
				case pb.MetadataFilter_OPERATOR_EQUAL:
					query = query.Where(datatypes.JSONQuery("metadata").Equals(val, mf.Key))
				case pb.MetadataFilter_OPERATOR_NOT_EQUAL:
					query = query.Not(datatypes.JSONQuery("metadata").Equals(val, mf.Key))
				case pb.MetadataFilter_OPERATOR_PARTIAL_MATCH:
					query = query.Where(datatypes.JSONQuery("metadata").Likes(string(mf.Value.Value), mf.Key))
				case pb.MetadataFilter_OPERATOR_GREATER_THAN:
					query = query.Where(clause.Expr{SQL: "? > ?", Vars: []interface{}{datatypes.JSONQuery("metadata").Extract(mf.Key), val}})
				case pb.MetadataFilter_OPERATOR_LESS_THAN:
					query = query.Where(clause.Expr{SQL: "? < ?", Vars: []interface{}{datatypes.JSONQuery("metadata").Extract(mf.Key), val}})
				case pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL:
					query = query.Where(clause.Expr{SQL: "? >= ?", Vars: []interface{}{datatypes.JSONQuery("metadata").Extract(mf.Key), val}})
				case pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL:
					query = query.Where(clause.Expr{SQL: "? <= ?", Vars: []interface{}{datatypes.JSONQuery("metadata").Extract(mf.Key), val}})
				}
			}
		}

		if filter.Currency != "" {
			query = query.Where("EXISTS (SELECT 1 FROM sql_postings WHERE sql_postings.transaction_id = sql_transactions.id AND sql_postings.currency = ?)", filter.Currency)
		}
	}

	var mtxns []SQLTransaction
	var count int64

	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}

	if pageSize > 0 {
		query = query.Limit(int(pageSize))
		if pageNumber > 0 {
			query = query.Offset(int((pageNumber - 1) * pageSize))
		}
	}

	if orderByDesc {
		query = query.Order("date DESC")
	} else {
		query = query.Order("date ASC")
	}

	if err := query.Preload("Postings").Find(&mtxns).Error; err != nil {
		return nil, 0, err
	}

	var results []*pb.Transaction
	for _, mt := range mtxns {
		var pbMeta map[string]*anypb.Any
		if len(mt.Metadata) > 0 {
			var metaMap map[string][]byte
			if err := json.Unmarshal(mt.Metadata, &metaMap); err == nil {
				pbMeta = make(map[string]*anypb.Any)
				for k, v := range metaMap {
					var anyVal anypb.Any
					_ = proto.Unmarshal(v, &anyVal)
					pbMeta[k] = &anyVal
				}
			}
		}

		var pbPostings []*pb.Posting
		for _, mp := range mt.Postings {
			pbPostings = append(pbPostings, &pb.Posting{
				Id:            mp.ID,
				TransactionId: mt.ID,
				Account:       accountfmt.ParseString(mp.AccountName),
				Amount:        moneyfmt.FromDecimal(mp.Amount, mp.Currency),
				Balance:       moneyfmt.FromDecimal(mp.Balance, mp.Currency),
				CreatedAt:     timestamppb.New(mp.CreatedAt),
			})
		}

		results = append(results, &pb.Transaction{
			Id:             mt.ID,
			IdempotencyKey: mt.IdempotencyKey,
			Date:           timestamppb.New(mt.Date),
			Note:           mt.Note,
			Metadata:       pbMeta,
			Postings:       pbPostings,
			CreatedAt:      timestamppb.New(mt.CreatedAt),
		})
	}

	return results, count, nil
}

func (r *sqlLedgerRepo) ListPostings(ctx context.Context, filter *pb.PostingFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Posting, int64, error) {
	query := r.db.WithContext(ctx).Model(&SQLPosting{})

	if filter != nil {
		if filter.TransactionFilter != nil {
			query = query.Joins("JOIN sql_transactions ON sql_transactions.id = sql_postings.transaction_id")
			tf := filter.TransactionFilter
			if tf.StartDate != nil && tf.EndDate != nil {
				query = query.Where("sql_transactions.date >= ? AND sql_transactions.date <= ?", tf.StartDate.AsTime(), tf.EndDate.AsTime())
			} else if tf.StartDate != nil {
				query = query.Where("sql_transactions.date >= ?", tf.StartDate.AsTime())
			} else if tf.EndDate != nil {
				query = query.Where("sql_transactions.date <= ?", tf.EndDate.AsTime())
			}

			if len(tf.Id) > 0 {
				query = query.Where("sql_transactions.id IN ?", tf.Id)
			}
			if len(tf.IdempotencyKey) > 0 {
				query = query.Where("sql_transactions.idempotency_key IN ?", tf.IdempotencyKey)
			}
			if tf.Note != "" {
				pattern := tf.Note
				if strings.Contains(pattern, "*") {
					pattern = strings.ReplaceAll(pattern, "*", "%")
					query = query.Where("sql_transactions.note LIKE ?", pattern)
				} else {
					query = query.Where("sql_transactions.note = ?", pattern)
				}
			}

			if len(tf.MetadataFilters) > 0 {
				for _, mf := range tf.MetadataFilters {
					var val interface{}
					if mf.Value != nil {
						valBytes, _ := proto.Marshal(mf.Value)
						val = valBytes
					}

					switch mf.Operator {
					case pb.MetadataFilter_OPERATOR_EQUAL:
						query = query.Where(datatypes.JSONQuery("sql_transactions.metadata").Equals(val, mf.Key))
					case pb.MetadataFilter_OPERATOR_NOT_EQUAL:
						query = query.Not(datatypes.JSONQuery("sql_transactions.metadata").Equals(val, mf.Key))
					case pb.MetadataFilter_OPERATOR_PARTIAL_MATCH:
						query = query.Where(datatypes.JSONQuery("sql_transactions.metadata").Likes(string(mf.Value.Value), mf.Key))
					case pb.MetadataFilter_OPERATOR_GREATER_THAN:
						query = query.Where(clause.Expr{SQL: "? > ?", Vars: []interface{}{datatypes.JSONQuery("sql_transactions.metadata").Extract(mf.Key), val}})
					case pb.MetadataFilter_OPERATOR_LESS_THAN:
						query = query.Where(clause.Expr{SQL: "? < ?", Vars: []interface{}{datatypes.JSONQuery("sql_transactions.metadata").Extract(mf.Key), val}})
					case pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL:
						query = query.Where(clause.Expr{SQL: "? >= ?", Vars: []interface{}{datatypes.JSONQuery("sql_transactions.metadata").Extract(mf.Key), val}})
					case pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL:
						query = query.Where(clause.Expr{SQL: "? <= ?", Vars: []interface{}{datatypes.JSONQuery("sql_transactions.metadata").Extract(mf.Key), val}})
					}
				}
			}

			if tf.Currency != "" {
				query = query.Where("sql_postings.currency = ?", tf.Currency)
			}
		}

		if len(filter.Id) > 0 {
			query = query.Where("sql_postings.id IN ?", filter.Id)
		}
		if len(filter.TransactionId) > 0 {
			query = query.Where("sql_postings.transaction_id IN ?", filter.TransactionId)
		}
		if filter.Account != nil {
			accStr := accountfmt.BuildString(filter.Account)
			if strings.Contains(accStr, "*") {
				pattern := strings.ReplaceAll(accStr, "*", "%")
				query = query.Where("sql_postings.account_name LIKE ?", pattern)
			} else {
				query = query.Where("sql_postings.account_name = ?", accStr)
			}
		}
	}

	var mpostings []SQLPosting
	var count int64

	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}

	if pageSize > 0 {
		query = query.Limit(int(pageSize))
		if pageNumber > 0 {
			query = query.Offset(int((pageNumber - 1) * pageSize))
		}
	}

	if orderByDesc {
		query = query.Order("sql_postings.created_at DESC")
	} else {
		query = query.Order("sql_postings.created_at ASC")
	}

	if err := query.Find(&mpostings).Error; err != nil {
		return nil, 0, err
	}

	var results []*pb.Posting
	for _, mp := range mpostings {
		results = append(results, &pb.Posting{
			Id:            mp.ID,
			TransactionId: mp.TransactionID,
			Account:       accountfmt.ParseString(mp.AccountName),
			Amount:        moneyfmt.FromDecimal(mp.Amount, mp.Currency),
			Balance:       moneyfmt.FromDecimal(mp.Balance, mp.Currency),
			CreatedAt:     timestamppb.New(mp.CreatedAt),
		})
	}

	return results, count, nil
}
