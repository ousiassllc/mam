package codex

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// A signed-out account and its usage limits switch together as seen from Status: a reader that runs while the account
// signs out never sees limits next to an account that is signed out.
func TestStatusNeverShowsUsageLimitsOfASignedOutAccount(t *testing.T) {
	s := New(Options{DataDir: t.TempDir()})
	t.Cleanup(s.Close)
	mins := int64(300)
	snap := protocol.RateLimitSnapshot{Primary: &protocol.RateLimitWindow{UsedPercent: 77, WindowDurationMins: &mins}}

	var stop atomic.Bool
	var torn atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if st := s.Status(); !st.Account.SignedIn && len(st.RateLimits) != 0 {
					torn.Add(1)
				}
			}
		}()
	}
	for range 300 {
		s.setAccount(AccountView{SignedIn: true, Type: protocol.AccountTypeChatGPT})
		s.setRateLimits(snap)
		s.setAccount(AccountView{SignedIn: false})
	}
	stop.Store(true)
	wg.Wait()
	if n := torn.Load(); n != 0 {
		t.Errorf("Status showed the usage limits of a signed-out account %d times", n)
	}
}
