package strands

import "testing"

func TestSlidingWindow_UnderLimit(t *testing.T) {
	mgr := &SlidingWindowManager{WindowSize: 10}
	msgs := []Message{UserMessage("a"), UserMessage("b"), UserMessage("c")}
	result := mgr.ReduceContext(msgs)
	if len(result) != 3 {
		t.Errorf("got %d messages, want 3 (no trimming)", len(result))
	}
}

func TestSlidingWindow_TrimsOldest(t *testing.T) {
	mgr := &SlidingWindowManager{WindowSize: 2}
	msgs := []Message{UserMessage("a"), UserMessage("b"), UserMessage("c"), UserMessage("d")}
	result := mgr.ReduceContext(msgs)
	if len(result) != 2 {
		t.Fatalf("got %d messages, want 2", len(result))
	}
	if result[0].Text() != "c" || result[1].Text() != "d" {
		t.Errorf("got [%q, %q], want [c, d]", result[0].Text(), result[1].Text())
	}
}

func TestSlidingWindow_ZeroWindowSize(t *testing.T) {
	mgr := &SlidingWindowManager{WindowSize: 0}
	msgs := []Message{UserMessage("a")}
	result := mgr.ReduceContext(msgs)
	if len(result) != 1 {
		t.Errorf("WindowSize=0 should not trim, got %d", len(result))
	}
}

func TestNullManager_NoChange(t *testing.T) {
	mgr := &NullManager{}
	msgs := []Message{UserMessage("a"), UserMessage("b")}
	result := mgr.ReduceContext(msgs)
	if len(result) != 2 {
		t.Errorf("NullManager should not modify messages, got %d", len(result))
	}
}
