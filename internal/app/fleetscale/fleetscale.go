// Package fleetscale reads the worker Auto Scaling Group's size and moves its
// cap. Optional: a deployment that does not set FLEET_AUTOSCALING_GROUP runs
// none of it.
package fleetscale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/rs/zerolog/log"
)

// CeilingTag is the group tag holding the highest cap an operator may set.
const CeilingTag = "warmbly:max-ceiling"

// WarnShare is the share of the cap at which the fleet is reported as close to it.
const WarnShare = 0.8

const (
	checkEvery = 5 * time.Minute
	warnEvery  = time.Hour
)

// GroupName is FLEET_AUTOSCALING_GROUP, the worker Auto Scaling Group's name.
func GroupName() string {
	return strings.TrimSpace(os.Getenv("FLEET_AUTOSCALING_GROUP"))
}

var (
	// ErrOutOfRange refuses a cap below the group's minimum or current size, or above its ceiling.
	ErrOutOfRange = errors.New("fleet cap out of range")
	// ErrNoCeiling refuses any change to a group without a readable ceiling tag.
	ErrNoCeiling = errors.New("fleet group has no " + CeilingTag + " tag")
)

// Status is the group's size as the admin panel shows it.
type Status struct {
	Group     string `json:"group"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Desired   int    `json:"desired"`
	InService int    `json:"in_service"`
	Pending   int    `json:"pending"`
	// Ceiling is the highest cap allowed; 0 when the tag is missing, which refuses every change.
	Ceiling int `json:"ceiling"`
}

// AtCap reports a fleet that cannot add another worker.
func (s Status) AtCap() bool { return s.Desired >= s.Max }

// NearCap reports a fleet within WarnShare of its cap.
func (s Status) NearCap() bool { return float64(s.Desired) >= WarnShare*float64(s.Max) }

// API is the part of the Auto Scaling client the service uses.
type API interface {
	DescribeAutoScalingGroups(ctx context.Context, in *autoscaling.DescribeAutoScalingGroupsInput, opts ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	UpdateAutoScalingGroup(ctx context.Context, in *autoscaling.UpdateAutoScalingGroupInput, opts ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error)
}

// Service reads and moves one group's cap.
type Service struct {
	api   API
	group string
}

// New uses the AWS default credential chain.
func New(ctx context.Context, group string) (*Service, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("fleet autoscaling: aws config: %w", err)
	}
	return NewWithAPI(autoscaling.NewFromConfig(cfg), group), nil
}

// NewWithAPI builds a Service over any client, for tests.
func NewWithAPI(api API, group string) *Service { return &Service{api: api, group: group} }

// Status reads the group's current size, cap and ceiling.
func (s *Service) Status(ctx context.Context) (Status, error) {
	out, err := s.api.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{AutoScalingGroupNames: []string{s.group}})
	if err != nil {
		return Status{}, err
	}
	if len(out.AutoScalingGroups) == 0 {
		return Status{}, fmt.Errorf("fleet autoscaling group %q not found", s.group)
	}
	g := out.AutoScalingGroups[0]
	st := Status{
		Group:   s.group,
		Min:     int(aws.ToInt32(g.MinSize)),
		Max:     int(aws.ToInt32(g.MaxSize)),
		Desired: int(aws.ToInt32(g.DesiredCapacity)),
	}
	for _, inst := range g.Instances {
		switch {
		case inst.LifecycleState == astypes.LifecycleStateInService:
			st.InService++
		case strings.HasPrefix(string(inst.LifecycleState), "Pending"):
			st.Pending++
		}
	}
	for _, tag := range g.Tags {
		if aws.ToString(tag.Key) == CeilingTag {
			if n, err := strconv.Atoi(strings.TrimSpace(aws.ToString(tag.Value))); err == nil && n > 0 {
				st.Ceiling = n
			}
		}
	}
	return st, nil
}

// SetMax moves the cap. It never goes below the group's minimum or its current
// size, so lowering it never terminates a worker, and never above the ceiling.
func (s *Service) SetMax(ctx context.Context, limit int) (Status, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if err := st.allows(limit); err != nil {
		return st, err
	}
	if _, err := s.api.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(s.group),
		MaxSize:              aws.Int32(int32(limit)),
	}); err != nil {
		return st, err
	}
	st.Max = limit
	return st, nil
}

func (s Status) allows(limit int) error {
	if s.Ceiling == 0 {
		return ErrNoCeiling
	}
	floor := max(1, s.Min, s.Desired)
	if limit < floor || limit > s.Ceiling {
		return fmt.Errorf("%w: choose %d to %d", ErrOutOfRange, floor, s.Ceiling)
	}
	return nil
}

// Watch logs a warning at most once an hour while the fleet sits at its cap,
// until ctx ends. Run it on one backend at a time.
func Watch(ctx context.Context, s *Service) {
	var warned time.Time
	for {
		st, err := s.Status(ctx)
		switch {
		case err != nil:
			log.Warn().Err(err).Str("group", s.group).Msg("fleet size not read")
		case st.AtCap() && time.Since(warned) >= warnEvery:
			log.Warn().Str("group", s.group).Int("desired", st.Desired).Int("max", st.Max).Int("ceiling", st.Ceiling).
				Msg("worker fleet is at its size cap and cannot grow; raise the cap in the admin panel")
			warned = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkEvery):
		}
	}
}
