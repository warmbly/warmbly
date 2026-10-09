//go:build kafka

package eventbus

import (
	"context"
	"errors"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

type kafkaDiagnosticAdmin interface {
	DescribeTopics(context.Context, ckf.TopicCollection, ...ckf.DescribeTopicsAdminOption) (ckf.DescribeTopicsResult, error)
	DescribeConsumerGroups(context.Context, []string, ...ckf.DescribeConsumerGroupsAdminOption) (ckf.DescribeConsumerGroupsResult, error)
	ListConsumerGroupOffsets(context.Context, []ckf.ConsumerGroupTopicPartitions, ...ckf.ListConsumerGroupOffsetsAdminOption) (ckf.ListConsumerGroupOffsetsResult, error)
	ListOffsets(context.Context, map[ckf.TopicPartition]ckf.OffsetSpec, ...ckf.ListOffsetsAdminOption) (ckf.ListOffsetsResult, error)
}

func (b *KafkaBus) Diagnose(ctx context.Context, at time.Time, scopes []DiagnosticScope) models.MonitoringSource {
	var admin kafkaDiagnosticAdmin
	if b != nil {
		b.topics.mu.Lock()
		if !b.topics.closed && b.topics.admin != nil {
			admin = b.topics.admin
		}
		b.topics.mu.Unlock()
	}
	return diagnoseKafka(ctx, admin, at, scopes)
}

func kafkaDiagnosticReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var e ckf.Error
	if errors.As(err, &e) {
		switch e.Code() {
		case ckf.ErrTopicAuthorizationFailed, ckf.ErrGroupAuthorizationFailed, ckf.ErrClusterAuthorizationFailed:
			return "permission_denied"
		case ckf.ErrUnknownTopicOrPart, ckf.ErrGroupIDNotFound:
			return "resource_absent"
		case ckf.ErrTimedOut, ckf.ErrTimedOutQueue:
			return "timeout"
		}
	}
	return "collection_failed"
}

func diagnoseKafka(ctx context.Context, admin kafkaDiagnosticAdmin, at time.Time, scopes []DiagnosticScope) models.MonitoringSource {
	out := models.MonitoringSource{Metrics: []models.MonitoringMetric{}}
	var measured int64
	for _, scope := range scopes {
		if admin == nil {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, "dependency_missing"))
			continue
		}
		lag, members, err := kafkaScopeLag(ctx, admin, scope)
		if err != nil {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, kafkaDiagnosticReason(err)))
			continue
		}
		measured++
		out.Metrics = append(out.Metrics, diagnosticMetric(scope, at, "committed_lag", "messages", "All requested topic partitions validated against earliest and latest offsets. Committed lag is not uncommitted processing, throughput or provider delivery.", lag), diagnosticMetric(scope, at, "members", "members", "Group membership can be absent during a rebalance; zero members alone is not an outage.", members))
	}
	diagnosticCoverage(&out, measured, int64(len(scopes)))
	return out
}

type diagnosticPartition struct {
	topic     string
	partition int32
}

func kafkaScopeLag(ctx context.Context, admin kafkaDiagnosticAdmin, scope DiagnosticScope) (int64, int64, error) {
	invalid := errors.New("incomplete broker observation")
	topics, err := admin.DescribeTopics(ctx, ckf.NewTopicCollectionOfTopicNames(scope.Topics))
	if err != nil {
		return 0, 0, err
	}
	if len(topics.TopicDescriptions) != len(scope.Topics) {
		return 0, 0, invalid
	}
	partitions := []ckf.TopicPartition{}
	expected := map[diagnosticPartition]bool{}
	for _, t := range topics.TopicDescriptions {
		if t.Error.Code() != ckf.ErrNoError {
			return 0, 0, t.Error
		}
		if len(t.Partitions) == 0 {
			return 0, 0, invalid
		}
		found := false
		for _, requested := range scope.Topics {
			if t.Name == requested {
				found = true
			}
		}
		if !found {
			return 0, 0, invalid
		}
		for _, p := range t.Partitions {
			name := t.Name
			key := diagnosticPartition{name, int32(p.Partition)}
			if expected[key] {
				return 0, 0, invalid
			}
			expected[key] = true
			partitions = append(partitions, ckf.TopicPartition{Topic: &name, Partition: int32(p.Partition)})
		}
	}
	if len(partitions) == 0 || len(partitions) > 128 {
		return 0, 0, invalid
	}
	groups, err := admin.DescribeConsumerGroups(ctx, []string{scope.Group})
	if err != nil {
		return 0, 0, err
	}
	if len(groups.ConsumerGroupDescriptions) != 1 || groups.ConsumerGroupDescriptions[0].GroupID != scope.Group {
		return 0, 0, invalid
	}
	g := groups.ConsumerGroupDescriptions[0]
	if g.Error.Code() != ckf.ErrNoError {
		return 0, 0, g.Error
	}
	committed, err := admin.ListConsumerGroupOffsets(ctx, []ckf.ConsumerGroupTopicPartitions{{Group: scope.Group, Partitions: partitions}})
	if err != nil {
		return 0, 0, err
	}
	if len(committed.ConsumerGroupsTopicPartitions) != 1 || committed.ConsumerGroupsTopicPartitions[0].Group != scope.Group {
		return 0, 0, invalid
	}
	offsets := map[diagnosticPartition]int64{}
	for _, p := range committed.ConsumerGroupsTopicPartitions[0].Partitions {
		if p.Error != nil {
			return 0, 0, p.Error
		}
		if p.Topic == nil || p.Offset < 0 {
			return 0, 0, invalid
		}
		key := diagnosticPartition{*p.Topic, p.Partition}
		if !expected[key] {
			return 0, 0, invalid
		}
		if _, exists := offsets[key]; exists {
			return 0, 0, invalid
		}
		offsets[key] = int64(p.Offset)
	}
	if len(offsets) != len(expected) {
		return 0, 0, invalid
	}
	specs := map[ckf.TopicPartition]ckf.OffsetSpec{}
	for _, p := range partitions {
		specs[p] = ckf.LatestOffsetSpec
	}
	latest, err := admin.ListOffsets(ctx, specs)
	if err != nil {
		return 0, 0, err
	}
	for _, p := range partitions {
		specs[p] = ckf.EarliestOffsetSpec
	}
	earliest, err := admin.ListOffsets(ctx, specs)
	if err != nil {
		return 0, 0, err
	}
	read := func(result ckf.ListOffsetsResult) (map[diagnosticPartition]int64, error) {
		values := map[diagnosticPartition]int64{}
		for p, v := range result.ResultInfos {
			if v.Error.Code() != ckf.ErrNoError {
				return nil, v.Error
			}
			if p.Topic == nil || v.Offset < 0 {
				return nil, invalid
			}
			key := diagnosticPartition{*p.Topic, p.Partition}
			if !expected[key] {
				return nil, invalid
			}
			if _, exists := values[key]; exists {
				return nil, invalid
			}
			values[key] = int64(v.Offset)
		}
		if len(values) != len(expected) {
			return nil, invalid
		}
		return values, nil
	}
	ends, err := read(latest)
	if err != nil {
		return 0, 0, err
	}
	starts, err := read(earliest)
	if err != nil {
		return 0, 0, err
	}
	var lag int64
	for key, offset := range offsets {
		if starts[key] > offset || offset > ends[key] || starts[key] > ends[key] {
			return 0, 0, invalid
		}
		delta := ends[key] - offset
		if delta > int64(^uint64(0)>>1)-lag {
			return 0, 0, invalid
		}
		lag += delta
	}
	return lag, int64(len(g.Members)), nil
}
