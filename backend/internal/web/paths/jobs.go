package paths

// Jobs is the list of the jobs that run and the latest that ended, "/jobs" (spec/components/jobs-screen.md). GET answers the page.
func Jobs() string { return "/jobs" }

// JobsList is "/jobs/list": GET answers the list alone, which the refresh button of the page swaps in.
func JobsList() string { return Jobs() + "/list" }
