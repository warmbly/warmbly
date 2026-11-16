package fleetscale

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

type fakeGroup struct {
	min, max, desired int32
	ceiling           string
	instances         []astypes.LifecycleState
	updates           []int32
}

func (f *fakeGroup) DescribeAutoScalingGroups(context.Context, *autoscaling.DescribeAutoScalingGroupsInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	g := astypes.AutoScalingGroup{MinSize: aws.Int32(f.min), MaxSize: aws.Int32(f.max), DesiredCapacity: aws.Int32(f.desired)}
	for _, state := range f.instances {
		g.Instances = append(g.Instances, astypes.Instance{LifecycleState: state})
	}
	if f.ceiling != "" {
		g.Tags = []astypes.TagDescription{{Key: aws.String(CeilingTag), Value: aws.String(f.ceiling)}}
	}
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: []astypes.AutoScalingGroup{g}}, nil
}

func (f *fakeGroup) UpdateAutoScalingGroup(_ context.Context, in *autoscaling.UpdateAutoScalingGroupInput, _ ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	if in.MinSize != nil || in.DesiredCapacity != nil {
		panic("only the cap may change")
	}
	f.updates = append(f.updates, aws.ToInt32(in.MaxSize))
	f.max = aws.ToInt32(in.MaxSize)
	return &autoscaling.UpdateAutoScalingGroupOutput{}, nil
}

func TestSetMaxStaysBetweenTheFleetAndTheCeiling(t *testing.T) {
	cases := []struct {
		name    string
		group   fakeGroup
		limit   int
		wantErr error
	}{
		{name: "raise within the ceiling", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "50"}, limit: 30},
		{name: "lower to the current size", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "50"}, limit: 3},
		{name: "up to the ceiling", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "50"}, limit: 50},
		{name: "above the ceiling", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "50"}, limit: 51, wantErr: ErrOutOfRange},
		{name: "below the running fleet", group: fakeGroup{min: 1, max: 10, desired: 4, ceiling: "50"}, limit: 3, wantErr: ErrOutOfRange},
		{name: "below the minimum", group: fakeGroup{min: 2, max: 10, desired: 2, ceiling: "50"}, limit: 1, wantErr: ErrOutOfRange},
		{name: "zero", group: fakeGroup{min: 0, max: 10, desired: 0, ceiling: "50"}, limit: 0, wantErr: ErrOutOfRange},
		{name: "no ceiling tag", group: fakeGroup{min: 1, max: 10, desired: 3}, limit: 11, wantErr: ErrNoCeiling},
		{name: "unreadable ceiling tag", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "lots"}, limit: 11, wantErr: ErrNoCeiling},
		{name: "ceiling tag beyond a group size", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "3000000000"}, limit: 11, wantErr: ErrNoCeiling},
		{name: "cap beyond a group size", group: fakeGroup{min: 1, max: 10, desired: 3, ceiling: "2147483647"}, limit: 3000000000, wantErr: ErrOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group := tc.group
			st, err := NewWithAPI(&group, "fleet").SetMax(context.Background(), tc.limit)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(group.updates) != 0 {
					t.Fatalf("a refused cap reached the group: %v", group.updates)
				}
				return
			}
			if len(group.updates) != 1 || int(group.updates[0]) != tc.limit || st.Max != tc.limit {
				t.Fatalf("updates = %v, status max = %d, want one update to %d", group.updates, st.Max, tc.limit)
			}
		})
	}
}

func TestStatusCountsWorkersAndReportsTheCap(t *testing.T) {
	group := &fakeGroup{min: 1, max: 5, desired: 5, ceiling: "50", instances: []astypes.LifecycleState{
		astypes.LifecycleStateInService, astypes.LifecycleStateInService, astypes.LifecycleStatePending,
		astypes.LifecycleStatePendingWait, astypes.LifecycleStateTerminating,
	}}
	st, err := NewWithAPI(group, "fleet").Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.InService != 2 || st.Pending != 2 || st.Ceiling != 50 || !st.AtCap() || !st.NearCap() {
		t.Fatalf("status = %+v, at cap %v, near cap %v", st, st.AtCap(), st.NearCap())
	}
	if (Status{Max: 10, Desired: 7}).NearCap() || !(Status{Max: 10, Desired: 8}).NearCap() {
		t.Fatal("near cap starts at 80% of the cap")
	}
}
