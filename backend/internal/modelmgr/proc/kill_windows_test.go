//go:build windows

package proc_test

// alive on Windows is not exercised: the Job Object's kill-on-close cannot be tested on the Linux development
// host, so the tests that ask whether the child is gone only see "gone" here.
func alive(int) bool { return false }
