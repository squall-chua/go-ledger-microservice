package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/squall-chua/gmqb"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mongoLedgerRepo struct {
	db       *mongo.Database
	txns     *gmqb.Collection[MongoTransaction]
	accounts *gmqb.Collection[MongoAccount]
}

func NewMongoLedgerRepository(db *mongo.Database) LedgerRepository {
	// Create indexes
	_, _ = db.Collection("transactions").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "idempotency_key", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return &mongoLedgerRepo{
		db:       db,
		txns:     gmqb.Wrap[MongoTransaction](db.Collection("transactions")),
		accounts: gmqb.Wrap[MongoAccount](db.Collection("accounts")),
	}
}

type MongoTransaction struct {
	ID             string         `bson:"_id"`
	IdempotencyKey string         `bson:"idempotency_key"`
	Date           time.Time      `bson:"date"`
	Note           string         `bson:"note"`
	Metadata       bson.M         `bson:"metadata,omitempty"`
	Postings       []MongoPosting `bson:"postings"`
	CreatedAt      time.Time      `bson:"created_at"`
}

type MongoPosting struct {
	ID          string          `bson:"id"`
	AccountName string          `bson:"account_name"`
	Amount      bson.Decimal128 `bson:"amount"`
	Balance     bson.Decimal128 `bson:"balance"`
	Currency    string          `bson:"currency"`
}

// Composite ID for MongoAccount to support multiple currencies
type AccountID struct {
	Name     string `bson:"name"`
	Currency string `bson:"currency"`
}

type MongoAccount struct {
	ID        AccountID       `bson:"_id"`
	Balance   bson.Decimal128 `bson:"balance"`
	UpdatedAt time.Time       `bson:"updated_at"`
}

func (r *mongoLedgerRepo) RecordTransaction(ctx context.Context, txn *pb.Transaction, verifyNonNegativeBalances []string) error {
	// Check idempotency first (to avoid starting a tx just to fail)
	query := gmqb.Eq("idempotency_key", txn.IdempotencyKey)
	_, err := r.txns.FindOne(ctx, query)
	if err == nil {
		return ErrIdempotentHit
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}

	session, err := r.db.Client().StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	callback := func(sessCtx context.Context) (interface{}, error) {
		// Verify Idempotency inside tx
		innerQuery := gmqb.Eq("idempotency_key", txn.IdempotencyKey)
		_, err := r.txns.FindOne(sessCtx, innerQuery)
		if err == nil {
			return nil, ErrIdempotentHit
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
				return nil, ErrCurrencyMismatch
			}
		}

		// Insert Postings and update Account Balances
		var embeddedPostings []MongoPosting
		for _, p := range txn.Postings {
			if p.Id == "" {
				p.Id = uuid.New().String()
			}
			p.TransactionId = txn.Id

			amountDec, err := moneyfmt.ToDecimal(p.Amount)
			if err != nil {
				return nil, err
			}
			amountBson, _ := moneyfmt.ToDecimal128(amountDec)

			// Upsert account running balance using composite ID
			opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
			update := bson.M{
				"$inc": bson.M{"balance": amountBson},
				"$set": bson.M{"updated_at": time.Now().UTC()},
			}
			var acc MongoAccount
			accNameStr := accountfmt.BuildString(p.Account)
			accID := AccountID{Name: accNameStr, Currency: p.Amount.CurrencyCode}
			accQuery := gmqb.Eq("_id", accID)
			err = r.accounts.Unwrap().FindOneAndUpdate(sessCtx, accQuery.BsonD(), update, opts).Decode(&acc)
			if err != nil {
				return nil, err
			}

			// Store the calculated running balance into the posting!
			p.Balance = &money.Money{}
			dbBal, _ := moneyfmt.FromDecimal128(acc.Balance)
			pbBal := moneyfmt.FromDecimal(dbBal, acc.ID.Currency)
			p.Balance.CurrencyCode = pbBal.CurrencyCode
			p.Balance.Units = pbBal.Units
			p.Balance.Nanos = pbBal.Nanos

			mp := MongoPosting{
				ID:          p.Id,
				AccountName: accNameStr,
				Amount:      amountBson,
				Balance:     acc.Balance,
				Currency:    p.Amount.CurrencyCode,
			}
			embeddedPostings = append(embeddedPostings, mp)
		}

		// Save Transaction
		metaBson := bson.M{}
		if txn.Metadata != nil {
			for k, v := range txn.Metadata {
				b, _ := proto.Marshal(v)
				metaBson[k] = b // Alternatively we could unmarshal to JSON, but storing raw bytes inside BSON for protobuf Any is safest or storing JSON. Let's store JSON strings.
				// For better querying we should probably store native values if possible. For now just raw.
			}
		}

		mt := MongoTransaction{
			ID:             txn.Id,
			IdempotencyKey: txn.IdempotencyKey,
			Date:           txn.Date.AsTime(),
			Note:           txn.Note,
			Metadata:       metaBson,
			Postings:       embeddedPostings,
			CreatedAt:      txn.CreatedAt.AsTime(),
		}
		_, err = r.txns.InsertOne(sessCtx, &mt)
		if err != nil {
			return nil, err
		}

		// Verify Non-Negative Balances using the updated balances inside the postings
		for _, vAccount := range verifyNonNegativeBalances {
			isWildcard := strings.HasSuffix(vAccount, "*")
			prefix := strings.TrimSuffix(vAccount, "*")

			for _, p := range embeddedPostings {
				match := false
				if isWildcard {
					match = strings.HasPrefix(p.AccountName, prefix)
				} else {
					match = (p.AccountName == vAccount)
				}

				if match {
					dbBal, _ := moneyfmt.FromDecimal128(p.Balance)
					if dbBal.IsNegative() {
						return nil, fmt.Errorf("account %s has negative balance: %s %s", p.AccountName, dbBal.String(), p.Currency)
					}
				}
			}
		}

		return nil, nil
	}

	_, err = session.WithTransaction(ctx, callback)
	if err != nil {
		// Local fallback if transactions are not supported by the replica config
		if strings.Contains(err.Error(), "Transaction") || strings.Contains(err.Error(), "replica set") {
			_, fallbackErr := callback(ctx)
			return fallbackErr
		}
		return err
	}
	return nil
}

func (r *mongoLedgerRepo) GetAccountBalance(ctx context.Context, accountName string, currencyFilter string) ([]*pb.AccountBalance, error) {
	// If exact match
	if !strings.Contains(accountName, "*") {
		// Must find all currencies for the given account name
		query := gmqb.Eq("_id.name", accountName)
		if currencyFilter != "" {
			query = gmqb.And(query, gmqb.Eq("_id.currency", currencyFilter))
		}
		accounts, err := r.accounts.Find(ctx, query)
		if err != nil {
			return nil, err
		}

		var result []*pb.AccountBalance
		for _, acc := range accounts {
			dbBal, _ := moneyfmt.FromDecimal128(acc.Balance)
			pbBal := &pb.AccountBalance{
				Account: accountfmt.ParseString(acc.ID.Name),
				Balance: moneyfmt.FromDecimal(dbBal, acc.ID.Currency),
			}
			if !acc.UpdatedAt.IsZero() {
				pbBal.UpdatedAt = timestamppb.New(acc.UpdatedAt)
			}
			result = append(result, pbBal)
		}

		return result, nil
	}

	// If wildcard match e.g. Assets:* or *:user:*
	pattern := strings.ReplaceAll(accountName, "*", ".*")
	query := gmqb.Regex("_id.name", "^"+pattern+"$", "")
	if currencyFilter != "" {
		query = gmqb.And(query, gmqb.Eq("_id.currency", currencyFilter))
	}
	accounts, err := r.accounts.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	var result []*pb.AccountBalance
	for _, acc := range accounts {
		dbBal, _ := moneyfmt.FromDecimal128(acc.Balance)
		pbBal := &pb.AccountBalance{
			Account: accountfmt.ParseString(acc.ID.Name),
			Balance: moneyfmt.FromDecimal(dbBal, acc.ID.Currency),
		}
		if !acc.UpdatedAt.IsZero() {
			pbBal.UpdatedAt = timestamppb.New(acc.UpdatedAt)
		}
		result = append(result, pbBal)
	}

	return result, nil
}

func (r *mongoLedgerRepo) ListTransactions(ctx context.Context, filter *pb.TransactionFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Transaction, int64, error) {
	var filters []gmqb.Filter
	if filter != nil {
		if filter.StartDate != nil || filter.EndDate != nil {
			if filter.StartDate != nil && filter.EndDate != nil {
				filters = append(filters, gmqb.Gte("date", filter.StartDate.AsTime()))
				filters = append(filters, gmqb.Lt("date", filter.EndDate.AsTime()))
			} else if filter.StartDate != nil {
				filters = append(filters, gmqb.Gte("date", filter.StartDate.AsTime()))
			} else {
				filters = append(filters, gmqb.Lt("date", filter.EndDate.AsTime()))
			}
		}

		if len(filter.Id) > 0 {
			ints := make([]interface{}, len(filter.Id))
			for i, v := range filter.Id {
				ints[i] = v
			}
			filters = append(filters, gmqb.In("_id", ints...))
		}
		if len(filter.IdempotencyKey) > 0 {
			ints := make([]interface{}, len(filter.IdempotencyKey))
			for i, v := range filter.IdempotencyKey {
				ints[i] = v
			}
			filters = append(filters, gmqb.In("idempotency_key", ints...))
		}
		if filter.Note != "" {
			pattern := filter.Note
			if strings.Contains(pattern, "*") {
				pattern = strings.ReplaceAll(pattern, "*", ".*")
				filters = append(filters, gmqb.Regex("note", "^"+pattern+"$", ""))
			} else {
				filters = append(filters, gmqb.Eq("note", pattern))
			}
		}

		if len(filter.MetadataFilters) > 0 {
			for _, mf := range filter.MetadataFilters {
				var val interface{}
				if mf.Value != nil {
					valBytes, _ := proto.Marshal(mf.Value)
					val = valBytes
				}

				mKey := "metadata." + mf.Key
				switch mf.Operator {
				case pb.MetadataFilter_OPERATOR_EQUAL:
					filters = append(filters, gmqb.Eq(mKey, val))
				case pb.MetadataFilter_OPERATOR_NOT_EQUAL:
					filters = append(filters, gmqb.Ne(mKey, val))
				case pb.MetadataFilter_OPERATOR_GREATER_THAN:
					filters = append(filters, gmqb.Gt(mKey, val))
				case pb.MetadataFilter_OPERATOR_LESS_THAN:
					filters = append(filters, gmqb.Lt(mKey, val))
				case pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL:
					filters = append(filters, gmqb.Gte(mKey, val))
				case pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL:
					filters = append(filters, gmqb.Lte(mKey, val))
				case pb.MetadataFilter_OPERATOR_PARTIAL_MATCH:
					filters = append(filters, gmqb.Regex(mKey, string(mf.Value.Value), "i"))
				}
			}
		}

		if filter.Currency != "" {
			filters = append(filters, gmqb.Eq("postings.currency", filter.Currency))
		}
	}

	sortOrder := 1
	if orderByDesc {
		sortOrder = -1
	}

	pipeline := gmqb.NewPipeline()
	if len(filters) > 1 {
		pipeline = pipeline.Match(gmqb.And(filters...))
	} else if len(filters) == 1 {
		pipeline = pipeline.Match(filters[0])
	}

	pipeline = pipeline.Sort(bson.D{{Key: "date", Value: sortOrder}})

	dataPipeline := gmqb.NewPipeline()
	if pageSize > 0 {
		if pageNumber > 0 {
			dataPipeline = dataPipeline.Skip(int64((pageNumber - 1) * pageSize))
		}
		dataPipeline = dataPipeline.Limit(int64(pageSize))
	}

	countPipeline := gmqb.NewPipeline().Count("total")

	facets := map[string]gmqb.Pipeline{
		"data":  dataPipeline,
		"count": countPipeline,
	}

	pipeline = pipeline.Facet(facets)

	// We need to execute the aggregate pipeline via raw driver here
	// since gmqb.Collection[T] doesn't natively map arbitrary facet output to a generic.
	cursor, err := r.txns.Unwrap().Aggregate(ctx, pipeline.BsonD())
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type FacetResult struct {
		Data  []MongoTransaction `bson:"data"`
		Count []struct {
			Total int64 `bson:"total"`
		} `bson:"count"`
	}

	var facetsResult []FacetResult
	if err := cursor.All(ctx, &facetsResult); err != nil {
		return nil, 0, err
	}

	if len(facetsResult) == 0 {
		return []*pb.Transaction{}, 0, nil
	}

	resultData := facetsResult[0]
	mtxns := resultData.Data
	var count int64 = 0
	if len(resultData.Count) > 0 {
		count = resultData.Count[0].Total
	}

	var results []*pb.Transaction
	for _, mt := range mtxns {
		var pbPostings []*pb.Posting
		for _, mp := range mt.Postings {
			amtDec, _ := moneyfmt.FromDecimal128(mp.Amount)
			balDec, _ := moneyfmt.FromDecimal128(mp.Balance)

			pbPostings = append(pbPostings, &pb.Posting{
				Id:            mp.ID,
				TransactionId: mt.ID,
				Account:       accountfmt.ParseString(mp.AccountName),
				Amount:        moneyfmt.FromDecimal(amtDec, mp.Currency),
				Balance:       moneyfmt.FromDecimal(balDec, mp.Currency),
				CreatedAt:     timestamppb.New(mt.CreatedAt),
			})
		}

		results = append(results, &pb.Transaction{
			Id:             mt.ID,
			IdempotencyKey: mt.IdempotencyKey,
			Date:           timestamppb.New(mt.Date),
			Note:           mt.Note,
			Postings:       pbPostings,
			CreatedAt:      timestamppb.New(mt.CreatedAt),
		})
	}

	return results, count, nil
}

func (r *mongoLedgerRepo) ListPostings(ctx context.Context, filter *pb.PostingFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Posting, int64, error) {
	// To list postings linearly, we must unwind the postings array over the transactions collection
	// Pipeline: $match (Transaction levels), $unwind postings, $match (Posting levels), facet

	var baseFilters []gmqb.Filter
	if filter != nil {
		if len(filter.TransactionId) > 0 {
			ints := make([]interface{}, len(filter.TransactionId))
			for i, v := range filter.TransactionId {
				ints[i] = v
			}
			baseFilters = append(baseFilters, gmqb.In("_id", ints...))
		}

		if filter.TransactionFilter != nil {
			tf := filter.TransactionFilter
			if tf.StartDate != nil || tf.EndDate != nil {
				if tf.StartDate != nil && tf.EndDate != nil {
					baseFilters = append(baseFilters, gmqb.Gte("date", tf.StartDate.AsTime()))
					baseFilters = append(baseFilters, gmqb.Lt("date", tf.EndDate.AsTime()))
				} else if tf.StartDate != nil {
					baseFilters = append(baseFilters, gmqb.Gte("date", tf.StartDate.AsTime()))
				} else {
					baseFilters = append(baseFilters, gmqb.Lt("date", tf.EndDate.AsTime()))
				}
			}

			if len(tf.Id) > 0 {
				ints := make([]interface{}, len(tf.Id))
				for i, v := range tf.Id {
					ints[i] = v
				}
				baseFilters = append(baseFilters, gmqb.In("_id", ints...))
			}

			if len(tf.IdempotencyKey) > 0 {
				ints := make([]interface{}, len(tf.IdempotencyKey))
				for i, v := range tf.IdempotencyKey {
					ints[i] = v
				}
				baseFilters = append(baseFilters, gmqb.In("idempotency_key", ints...))
			}

			if tf.Note != "" {
				pattern := tf.Note
				if strings.Contains(pattern, "*") {
					pattern = strings.ReplaceAll(pattern, "*", ".*")
					baseFilters = append(baseFilters, gmqb.Regex("note", "^"+pattern+"$", ""))
				} else {
					baseFilters = append(baseFilters, gmqb.Eq("note", pattern))
				}
			}

			if len(tf.MetadataFilters) > 0 {
				for _, mf := range tf.MetadataFilters {
					var val interface{}
					if mf.Value != nil {
						valBytes, _ := proto.Marshal(mf.Value)
						val = valBytes
					}

					mKey := "metadata." + mf.Key
					switch mf.Operator {
					case pb.MetadataFilter_OPERATOR_EQUAL:
						baseFilters = append(baseFilters, gmqb.Eq(mKey, val))
					case pb.MetadataFilter_OPERATOR_NOT_EQUAL:
						baseFilters = append(baseFilters, gmqb.Ne(mKey, val))
					case pb.MetadataFilter_OPERATOR_GREATER_THAN:
						baseFilters = append(baseFilters, gmqb.Gt(mKey, val))
					case pb.MetadataFilter_OPERATOR_LESS_THAN:
						baseFilters = append(baseFilters, gmqb.Lt(mKey, val))
					case pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL:
						baseFilters = append(baseFilters, gmqb.Gte(mKey, val))
					case pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL:
						baseFilters = append(baseFilters, gmqb.Lte(mKey, val))
					case pb.MetadataFilter_OPERATOR_PARTIAL_MATCH:
						baseFilters = append(baseFilters, gmqb.Regex(mKey, string(mf.Value.Value), "i"))
					}
				}
			}

			if tf.Currency != "" {
				baseFilters = append(baseFilters, gmqb.Eq("postings.currency", tf.Currency))
			}
		}
	}

	pipeline := gmqb.NewPipeline()
	if len(baseFilters) > 1 {
		pipeline = pipeline.Match(gmqb.And(baseFilters...))
	} else if len(baseFilters) == 1 {
		pipeline = pipeline.Match(baseFilters[0])
	}

	// Unwind
	pipeline = pipeline.Unwind("$postings")

	// Secondary match on posting properties
	var postingFilters []gmqb.Filter
	if filter != nil {
		if len(filter.Id) > 0 {
			ints := make([]interface{}, len(filter.Id))
			for i, v := range filter.Id {
				ints[i] = v
			}
			postingFilters = append(postingFilters, gmqb.In("postings.id", ints...))
		}

		if filter.Account != nil {
			accStr := accountfmt.BuildString(filter.Account)
			if strings.Contains(accStr, "*") {
				pattern := strings.ReplaceAll(accStr, "*", ".*")
				postingFilters = append(postingFilters, gmqb.Regex("postings.account_name", "^"+pattern+"$", ""))
			} else {
				postingFilters = append(postingFilters, gmqb.Eq("postings.account_name", accStr))
			}
		}
	}

	if len(postingFilters) > 1 {
		pipeline = pipeline.Match(gmqb.And(postingFilters...))
	} else if len(postingFilters) == 1 {
		pipeline = pipeline.Match(postingFilters[0])
	}

	sortOrder := 1
	if orderByDesc {
		sortOrder = -1
	}

	dataPipeline := gmqb.NewPipeline().Sort(bson.D{{Key: "created_at", Value: sortOrder}})

	if pageSize > 0 {
		if pageNumber > 0 {
			dataPipeline = dataPipeline.Skip(int64((pageNumber - 1) * pageSize))
		}
		dataPipeline = dataPipeline.Limit(int64(pageSize))
	}

	countPipeline := gmqb.NewPipeline().Count("total")

	facets := map[string]gmqb.Pipeline{
		"data":  dataPipeline,
		"count": countPipeline,
	}

	pipeline = pipeline.Facet(facets)

	cursor, err := r.txns.Unwrap().Aggregate(ctx, pipeline.BsonD())
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type UnwoundTransaction struct {
		ID             string       `bson:"_id"`
		IdempotencyKey string       `bson:"idempotency_key"`
		Date           time.Time    `bson:"date"`
		Note           string       `bson:"note"`
		Metadata       bson.M       `bson:"metadata,omitempty"`
		Postings       MongoPosting `bson:"postings"`
		CreatedAt      time.Time    `bson:"created_at"`
	}

	type FacetResult struct {
		Data  []UnwoundTransaction `bson:"data"`
		Count []struct {
			Total int64 `bson:"total"`
		} `bson:"count"`
	}

	var facetsResult []FacetResult
	if err = cursor.All(ctx, &facetsResult); err != nil {
		return nil, 0, err
	}

	if len(facetsResult) == 0 {
		return nil, 0, nil
	}

	resultData := facetsResult[0]
	// Due to unwind, these "Transactions" only contain 1 element in their Postings array per record match!
	mtxns := resultData.Data
	var count int64 = 0
	if len(resultData.Count) > 0 {
		count = resultData.Count[0].Total
	}

	var results []*pb.Posting
	for _, mt := range mtxns {
		mp := mt.Postings // Because it was unwound, Postings is a single object
		amtDec, _ := moneyfmt.FromDecimal128(mp.Amount)
		balDec, _ := moneyfmt.FromDecimal128(mp.Balance)

		results = append(results, &pb.Posting{
			Id:            mp.ID,
			TransactionId: mt.ID,
			Account:       accountfmt.ParseString(mp.AccountName),
			Amount:        moneyfmt.FromDecimal(amtDec, mp.Currency),
			Balance:       moneyfmt.FromDecimal(balDec, mp.Currency),
			CreatedAt:     timestamppb.New(mt.CreatedAt), // Posting uses Transaction creation time inside DB
		})
	}

	return results, count, nil
}
