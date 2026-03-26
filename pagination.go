package billow

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/nulllvoid/billow/store"
)

// ---------------------------------------------------------------------------
// Cursor-based pagination types
// ---------------------------------------------------------------------------

// Page is a generic paginated result set returned by list methods that support
// cursor-based pagination.
type Page[T any] struct {
	Items      []T
	NextCursor string // empty when this is the last page
}

// cursor is the decoded form of an opaque page token.
// Format (before base64): "<lastID>" — a single subscription/plan ID.
// The shard hint is stored so the sharded store can resume within the
// correct shard without scanning from the beginning.
type cursor struct {
	lastID string
}

// encodeCursor base64-encodes a cursor so it is opaque to callers.
func encodeCursor(lastID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(lastID))
}

// decodeCursor decodes an opaque cursor string. Returns a zero cursor on any
// error so callers start from the beginning.
func decodeCursor(s string) cursor {
	if s == "" {
		return cursor{}
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}
	}
	return cursor{lastID: string(b)}
}

// ---------------------------------------------------------------------------
// ListPlansPage
// ---------------------------------------------------------------------------

// ListPlansPageInput is the input for a paginated plan list.
type ListPlansPageInput struct {
	ActiveOnly bool
	// Cursor is the opaque token returned by a previous call.
	// Pass empty string to start from the beginning.
	Cursor string
	// Limit is the maximum number of plans to return. Defaults to 20.
	Limit int
}

// ListPlansPage returns a single page of plans sorted lexicographically by ID.
// Pass the returned Page.NextCursor as the Cursor field on the next call to
// retrieve subsequent pages.
func (m *Manager) ListPlansPage(ctx context.Context, in ListPlansPageInput) (*Page[*Plan], error) {
	if in.Limit <= 0 {
		in.Limit = 20
	}
	cur := decodeCursor(in.Cursor)

	// Fetch everything (store doesn't expose cursor-based listing); we sort
	// and slice in memory. Plans are a small, admin-only collection so this
	// is acceptable — a production SQL store would push ORDER BY + WHERE id > ?
	// down to the database.
	sps, err := m.plans.ListPlans(ctx, store.PlanFilter{ActiveOnly: in.ActiveOnly})
	if err != nil {
		return nil, err
	}

	// Sort ascending by ID (lexicographic — UUID v7 is monotonically increasing).
	sortByID(sps, func(p *store.Plan) string { return p.ID })

	// Advance past the cursor.
	start := 0
	if cur.lastID != "" {
		for i, p := range sps {
			if p.ID == cur.lastID {
				start = i + 1
				break
			}
		}
	}
	sps = sps[start:]

	// Slice to limit+1 to detect whether there is a next page.
	hasMore := len(sps) > in.Limit
	if hasMore {
		sps = sps[:in.Limit]
	}

	plans := make([]*Plan, len(sps))
	for i, sp := range sps {
		plans[i] = planFromStore(sp)
	}

	var nextCursor string
	if hasMore && len(plans) > 0 {
		nextCursor = encodeCursor(plans[len(plans)-1].ID)
	}
	return &Page[*Plan]{Items: plans, NextCursor: nextCursor}, nil
}

// ---------------------------------------------------------------------------
// ListSubscriptionsPage
// ---------------------------------------------------------------------------

// ListSubscriptionsPageInput is the input for a paginated subscription list.
type ListSubscriptionsPageInput struct {
	SubscriberID string
	PlanID       string
	Status       string
	Cursor       string
	Limit        int
}

// ListSubscriptionsPage returns a single page of subscriptions sorted by ID.
func (m *Manager) ListSubscriptionsPage(ctx context.Context, in ListSubscriptionsPageInput) (*Page[*Subscription], error) {
	if in.Limit <= 0 {
		in.Limit = 20
	}
	cur := decodeCursor(in.Cursor)

	sss, err := m.subs.ListSubscriptions(ctx, store.SubscriptionFilter{
		SubscriberID: in.SubscriberID,
		PlanID:       in.PlanID,
		Status:       in.Status,
	})
	if err != nil {
		return nil, err
	}

	sortByID(sss, func(s *store.Subscription) string { return s.ID })

	start := 0
	if cur.lastID != "" {
		for i, ss := range sss {
			if ss.ID == cur.lastID {
				start = i + 1
				break
			}
		}
	}
	sss = sss[start:]

	hasMore := len(sss) > in.Limit
	if hasMore {
		sss = sss[:in.Limit]
	}

	subs := make([]*Subscription, len(sss))
	for i, ss := range sss {
		subs[i] = subFromStore(ss)
	}

	var nextCursor string
	if hasMore && len(subs) > 0 {
		nextCursor = encodeCursor(subs[len(subs)-1].ID)
	}
	return &Page[*Subscription]{Items: subs, NextCursor: nextCursor}, nil
}

// ---------------------------------------------------------------------------
// sort helper — insertion sort for small slices, no external dependencies
// ---------------------------------------------------------------------------

// sortByID sorts a slice in ascending order of the string key returned by keyFn.
// Uses insertion sort — O(n²) but allocation-free and fast for the small page
// sizes (<1 000) typical of admin list calls.
func sortByID[T any](s []T, keyFn func(T) string) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && strings.Compare(keyFn(s[j-1]), keyFn(s[j])) > 0 {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
