//go:build kafka

package eventbus

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeDiagnosticAdmin struct {
	problem string
	calls   []string
}

func (f *fakeDiagnosticAdmin) DescribeTopics(_ context.Context, _ ckf.TopicCollection, _ ...ckf.DescribeTopicsAdminOption) (ckf.DescribeTopicsResult, error) {
	f.calls = append(f.calls, "topics")
	r := ckf.DescribeTopicsResult{TopicDescriptions: []ckf.TopicDescription{{Name: "jobs.worker-events", Partitions: []ckf.TopicPartitionInfo{{Partition: 0}}}}}
	if f.problem == "topic_denied" {
		r.TopicDescriptions[0].Error = ckf.NewError(ckf.ErrTopicAuthorizationFailed, "private broker hostname", false)
	}
	if f.problem == "no_topic" {
		r.TopicDescriptions = nil
	}
	return r, nil
}
func (f *fakeDiagnosticAdmin) DescribeConsumerGroups(_ context.Context, _ []string, _ ...ckf.DescribeConsumerGroupsAdminOption) (ckf.DescribeConsumerGroupsResult, error) {
	f.calls = append(f.calls, "groups")
	r := ckf.DescribeConsumerGroupsResult{ConsumerGroupDescriptions: []ckf.ConsumerGroupDescription{{GroupID: "private-group", Members: []ckf.MemberDescription{{}}}}}
	if f.problem == "group_denied" {
		r.ConsumerGroupDescriptions[0].Error = ckf.NewError(ckf.ErrGroupAuthorizationFailed, "private broker hostname", false)
	}
	if f.problem == "no_group" {
		r.ConsumerGroupDescriptions = nil
	}
	return r, nil
}
func (f *fakeDiagnosticAdmin) ListConsumerGroupOffsets(_ context.Context, _ []ckf.ConsumerGroupTopicPartitions, _ ...ckf.ListConsumerGroupOffsetsAdminOption) (ckf.ListConsumerGroupOffsetsResult, error) {
	f.calls = append(f.calls, "committed")
	topic := "jobs.worker-events"
	p := ckf.TopicPartition{Topic: &topic, Partition: 0, Offset: 4}
	r := ckf.ListConsumerGroupOffsetsResult{ConsumerGroupsTopicPartitions: []ckf.ConsumerGroupTopicPartitions{{Group: "private-group", Partitions: []ckf.TopicPartition{p}}}}
	switch f.problem {
	case "no_commit":
		r.ConsumerGroupsTopicPartitions[0].Partitions = nil
	case "negative":
		r.ConsumerGroupsTopicPartitions[0].Partitions[0].Offset = -1001
	case "ahead":
		r.ConsumerGroupsTopicPartitions[0].Partitions[0].Offset = 12
	case "behind_retention":
		r.ConsumerGroupsTopicPartitions[0].Partitions[0].Offset = 0
	case "partition_denied":
		r.ConsumerGroupsTopicPartitions[0].Partitions[0].Error = ckf.NewError(ckf.ErrTopicAuthorizationFailed, "private", false)
	case "commit_top_error":
		return r, context.DeadlineExceeded
	}
	return r, nil
}
func (f *fakeDiagnosticAdmin) ListOffsets(_ context.Context, specs map[ckf.TopicPartition]ckf.OffsetSpec, _ ...ckf.ListOffsetsAdminOption) (ckf.ListOffsetsResult, error) {
	r := ckf.ListOffsetsResult{ResultInfos: map[ckf.TopicPartition]ckf.ListOffsetsResultInfo{}}
	for p, spec := range specs {
		if spec == ckf.LatestOffsetSpec {
			f.calls = append(f.calls, "latest")
			r.ResultInfos[p] = ckf.ListOffsetsResultInfo{Offset: 10}
		} else {
			f.calls = append(f.calls, "earliest")
			r.ResultInfos[p] = ckf.ListOffsetsResultInfo{Offset: 2}
		}
	}
	switch f.problem {
	case "missing_end":
		r.ResultInfos = map[ckf.TopicPartition]ckf.ListOffsetsResultInfo{}
	case "end_denied":
		for p, v := range r.ResultInfos {
			v.Error = ckf.NewError(ckf.ErrTopicAuthorizationFailed, "private", false)
			r.ResultInfos[p] = v
		}
	case "negative_end":
		for p, v := range r.ResultInfos {
			v.Offset = -1
			r.ResultInfos[p] = v
		}
	case "end_top_error":
		return r, context.DeadlineExceeded
	}
	return r, nil
}

func TestMonitoringKafkaNeverClampsMissingInvalidOrDeniedOffsets(t *testing.T) {
	scope := DiagnosticScope{ID: "worker_events", Group: "private-group", Topics: []string{"jobs.worker-events"}}
	for _, problem := range []string{"", "topic_denied", "group_denied", "no_topic", "no_group", "no_commit", "negative", "ahead", "behind_retention", "partition_denied", "commit_top_error", "missing_end", "end_denied", "negative_end", "end_top_error"} {
		t.Run(problem, func(t *testing.T) {
			admin := &fakeDiagnosticAdmin{problem: problem}
			out := diagnoseKafka(t.Context(), admin, time.Now(), []DiagnosticScope{scope})
			if problem == "" {
				if *out.Metrics[0].Count != 6 || *out.Metrics[1].Count != 1 || out.Coverage != "complete" || strings.Join(admin.calls, ",") != "topics,groups,committed,latest,earliest" {
					t.Fatal(out, admin.calls)
				}
			} else if len(out.Metrics) != 1 || out.Metrics[0].Availability != models.MonitoringUnavailable || out.Metrics[0].Count != nil || out.Coverage != "unavailable" {
				t.Fatal("invalid observation became zero lag", out)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "private") {
				t.Fatal("broker credentials/identities leaked", string(raw))
			}
		})
	}
	var bus *KafkaBus
	out := bus.Diagnose(t.Context(), time.Now(), []DiagnosticScope{scope})
	if out.Metrics[0].Count != nil || out.Metrics[0].Reason != "dependency_missing" {
		t.Fatal(out)
	}
}

func TestKafkaPartitionEvidenceIsExplicitBoundedAndTimestamped(t *testing.T) {
	at := time.Now().UTC()
	scope := DiagnosticScope{ID: "worker_events", Group: "private-group", Topics: []string{"jobs.worker-events"}, IncludePartitions: true}
	out := diagnoseKafka(t.Context(), &fakeDiagnosticAdmin{}, at, []DiagnosticScope{scope})
	if out.Availability != models.MonitoringFresh || out.ObservedAt == nil || !out.ObservedAt.Equal(at) {
		t.Fatal("missing observation time", out)
	}
	b := out.Metrics[0].Broker
	if b == nil || len(b.Partitions) != 1 || b.ConsumerGroup != scope.Group || b.Partitions[0].CommittedLag != *out.Metrics[0].Count || b.Partitions[0].Latest-b.Partitions[0].Committed != b.Partitions[0].CommittedLag {
		t.Fatal("missing validated partition offsets", b)
	}
	out = diagnoseKafka(t.Context(), &fakeDiagnosticAdmin{problem: "no_commit"}, at, []DiagnosticScope{scope})
	if out.Metrics[0].Broker != nil || out.Metrics[0].Count != nil || out.Metrics[0].ObservedAt == nil {
		t.Fatal("missing offsets became healthy or unobserved", out)
	}
}
