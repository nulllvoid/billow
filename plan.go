package billow

import (
	"context"
	"time"

	"github.com/nulllvoid/billow/store"
)

// CreatePlanInput is the input for creating a new subscription plan.
type CreatePlanInput struct {
	Name          string
	Description   string
	Amount        int64             // smallest currency unit
	Currency      string
	Interval      PlanInterval
	IntervalCount int               // defaults to 1
	TrialDays     int
	Features      []string
	Limits        map[string]int64  // metric → limit (0 = unlimited)
	Metadata      map[string]string
}

// UpdatePlanInput holds the fields that can be updated after creation.
// Only non-zero / non-nil fields are applied (patch semantics).
type UpdatePlanInput struct {
	Name        *string
	Description *string
	Features    []string
	Limits      map[string]int64
	Metadata    map[string]string
	Active      *bool
}

// CreatePlan creates a new plan, persists it, and registers it with the
// payment provider (if one is configured).
func (m *Manager) CreatePlan(ctx context.Context, in CreatePlanInput) (*Plan, error) {
	if in.IntervalCount == 0 {
		in.IntervalCount = 1
	}

	now := time.Now().UTC()
	plan := &Plan{
		ID:            m.newID(),
		Name:          in.Name,
		Description:   in.Description,
		Amount:        in.Amount,
		Currency:      in.Currency,
		Interval:      in.Interval,
		IntervalCount: in.IntervalCount,
		TrialDays:     in.TrialDays,
		Features:      in.Features,
		Limits:        in.Limits,
		Metadata:      in.Metadata,
		Active:        true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Register with payment provider if available.
	if m.provider != nil {
		providerID, err := m.provider.CreatePlan(ctx, planToProviderPlan(plan))
		if err != nil {
			return nil, err
		}
		plan.ProviderID = providerID
	}

	if err := m.plans.SavePlan(ctx, planToStore(plan)); err != nil {
		return nil, err
	}
	return plan, nil
}

// GetPlan returns a plan by its internal ID.
func (m *Manager) GetPlan(ctx context.Context, id string) (*Plan, error) {
	sp, err := m.plans.GetPlan(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	return planFromStore(sp), nil
}

// ListPlans returns all plans. Pass activeOnly = true to exclude archived plans.
func (m *Manager) ListPlans(ctx context.Context, activeOnly bool) ([]*Plan, error) {
	sps, err := m.plans.ListPlans(ctx, store.PlanFilter{ActiveOnly: activeOnly})
	if err != nil {
		return nil, err
	}
	plans := make([]*Plan, len(sps))
	for i, sp := range sps {
		plans[i] = planFromStore(sp)
	}
	return plans, nil
}

// UpdatePlan applies patch-style updates to an existing plan and syncs the
// mutable fields with the payment provider.
func (m *Manager) UpdatePlan(ctx context.Context, id string, in UpdatePlanInput) (*Plan, error) {
	sp, err := m.plans.GetPlan(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}

	if in.Name != nil {
		sp.Name = *in.Name
	}
	if in.Description != nil {
		sp.Description = *in.Description
	}
	if in.Features != nil {
		sp.Features = in.Features
	}
	if in.Limits != nil {
		sp.Limits = in.Limits
	}
	if in.Metadata != nil {
		sp.Metadata = in.Metadata
	}
	if in.Active != nil {
		sp.Active = *in.Active
	}
	sp.UpdatedAt = time.Now().UTC()

	plan := planFromStore(sp)

	if m.provider != nil && sp.ProviderID != "" {
		if err := m.provider.UpdatePlan(ctx, planToProviderPlan(plan)); err != nil {
			return nil, err
		}
	}

	if err := m.plans.SavePlan(ctx, planToStore(plan)); err != nil {
		return nil, err
	}
	return plan, nil
}

// DeletePlan deactivates and removes a plan.
// Existing subscriptions on the plan are not affected.
func (m *Manager) DeletePlan(ctx context.Context, id string) error {
	sp, err := m.plans.GetPlan(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return ErrPlanNotFound
		}
		return err
	}

	if m.provider != nil && sp.ProviderID != "" {
		if err := m.provider.DeletePlan(ctx, sp.ProviderID); err != nil {
			return err
		}
	}

	return m.plans.DeletePlan(ctx, id)
}
