package mongosync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// collections maps an event kind to its Mongo collection (same names, one per kind).
var collections = map[string]string{
	KindBests:  "personal_bests",
	KindFamily: "family_board",
	KindStats:  "progress_stats",

	KindQuestionBank:  "question_bank",
	KindQuestionAudit: "question_audit",
	KindQuestionStats: "question_stats",
	KindContentWorld:  "content_worlds",
	KindContentQuest:  "content_quests",
}

// Mongo is the real Sink (Spring: a MongoRepository). Documents are replaced by their unique "key" field,
// so sending the same Op twice leaves one identical document.
type Mongo struct {
	client *mongo.Client
	db     *mongo.Database

	mu      sync.Mutex // guards indexed
	indexed bool       // true once the unique indexes exist
}

// NewMongo connects lazily (the driver dials in the background, so an unreachable Atlas does not stop the app)
// and tries to create the unique indexes right away. If that fails the sink retries on the first Bulk.
func NewMongo(ctx context.Context, uri, dbName string) (*Mongo, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	m := &Mongo{client: client, db: client.Database(dbName)}
	ictx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_ = m.ensureIndexes(ictx) // error kept in m.indexed=false; Bulk retries and reports it
	return m, nil
}

// ensureIndexes creates a unique index on "key" in each collection. CreateOne is a no-op when it already exists.
func (m *Mongo) ensureIndexes(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.indexed {
		return nil
	}
	for _, name := range collections {
		_, err := m.db.Collection(name).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "key", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_key"),
		})
		if err != nil {
			return fmt.Errorf("create index on %s: %w", name, err)
		}
	}
	m.indexed = true
	return nil
}

// Upsert writes one Op.
func (m *Mongo) Upsert(ctx context.Context, op Op) error { return m.Bulk(ctx, []Op{op}) }

// Bulk writes many Ops, one BulkWrite per collection, unordered (the worker already de-duplicates keys).
func (m *Mongo) Bulk(ctx context.Context, ops []Op) error {
	if err := m.ensureIndexes(ctx); err != nil {
		return err
	}
	byColl := map[string][]mongo.WriteModel{}
	for _, op := range ops {
		name, ok := collections[op.Kind]
		if !ok {
			return fmt.Errorf("unknown event kind %q", op.Kind)
		}
		filter := bson.D{{Key: "key", Value: op.Key}}
		if op.Delete && op.DeleteBy != "" {
			byColl[name] = append(byColl[name], mongo.NewDeleteManyModel().SetFilter(bson.D{{Key: op.DeleteBy, Value: op.Key}}))
			continue
		}
		if op.Delete {
			byColl[name] = append(byColl[name], mongo.NewDeleteOneModel().SetFilter(filter))
			continue
		}
		doc := make(map[string]any, len(op.Doc)+1) // copy so the caller's map is never changed
		for k, v := range op.Doc {
			doc[k] = v
		}
		doc["key"] = op.Key
		byColl[name] = append(byColl[name], mongo.NewReplaceOneModel().SetFilter(filter).SetReplacement(doc).SetUpsert(true))
	}
	for name, models := range byColl {
		if _, err := m.db.Collection(name).BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("bulk write %s: %w", name, err)
		}
	}
	return nil
}

// Close disconnects the driver.
func (m *Mongo) Close(ctx context.Context) error { return m.client.Disconnect(ctx) }
