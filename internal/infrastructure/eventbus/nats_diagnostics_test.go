package eventbus

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMonitoringNATSExistingConsumerReadOnlyAndAbsentScopes(t *testing.T) {
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	t.Cleanup(s.Shutdown)
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats not ready")
	}
	nc, err := nats.Connect(s.ClientURL(), nats.NoReconnect())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(t.Context(), jetstream.StreamConfig{Name: "monitoring", Subjects: []string{"warmbly.>"}, Storage: jetstream.MemoryStorage})
	if err != nil {
		t.Fatal(err)
	}
	b := &NATSBus{nc: nc, js: js, stream: "monitoring"}
	scope := DiagnosticScope{ID: "worker_events", Group: "consumer-group", Topics: []string{"jobs.worker-events"}}
	consumer, err := stream.CreateConsumer(t.Context(), jetstream.ConsumerConfig{Durable: b.durable(scope.Group, scope.Topics), AckPolicy: jetstream.AckExplicitPolicy, FilterSubject: "warmbly.jobs.worker-events"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := js.Publish(t.Context(), "warmbly.jobs.worker-events", []byte("private payload")); err != nil {
			t.Fatal(err)
		}
	}
	before, err := consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	streamBefore, err := stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result := b.Diagnose(t.Context(), time.Now(), []DiagnosticScope{scope, {ID: "missing", Group: "absent", Topics: scope.Topics}})
	if result.Coverage != "partial" || *result.MeasuredScopes != 1 || *result.ExpectedScopes != 2 || len(result.Metrics) != 4 {
		t.Fatal(result)
	}
	for i, count := range []int64{2, 0, 0} {
		if result.Metrics[i].Count == nil || *result.Metrics[i].Count != count {
			t.Fatal(result.Metrics)
		}
	}
	if result.Metrics[3].Availability != models.MonitoringUnavailable || result.Metrics[3].Reason != "resource_absent" || result.Metrics[3].Count != nil {
		t.Fatal(result.Metrics[3])
	}
	after, err := consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	streamAfter, err := stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Config, after.Config) || !reflect.DeepEqual(before.Delivered, after.Delivered) || !reflect.DeepEqual(before.AckFloor, after.AckFloor) || before.NumPending != after.NumPending || before.NumAckPending != after.NumAckPending || !reflect.DeepEqual(streamBefore.State, streamAfter.State) {
		t.Fatal("diagnostics altered stream or delivery state")
	}
	var absent *NATSBus
	if result := absent.Diagnose(context.Background(), time.Now(), []DiagnosticScope{scope}); result.Metrics[0].Count != nil || result.Metrics[0].Reason != "dependency_missing" {
		t.Fatal(result)
	}
}

func TestMonitoringDiagnosticsContainOnlyReadAPIs(t *testing.T) {
	for _, path := range []string{"nats_diagnostics.go", "kafka_diagnostics.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		forbidden := map[string]bool{"Subscribe": true, "SubscribeTopics": true, "Fetch": true, "Consume": true, "Ack": true, "Nak": true, "Publish": true, "CreateConsumer": true, "CreateOrUpdateConsumer": true, "CreateStream": true, "CreateOrUpdateStream": true, "CreateTopics": true, "AlterConsumerGroupOffsets": true, "NewConsumer": true, "NewAdminClient": true, "Reconnect": true, "ensureStream": true, "adminClient": true}
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && forbidden[sel.Sel.Name] {
					t.Errorf("%s calls forbidden %s", path, sel.Sel.Name)
				}
			}
			return true
		})
	}
}
