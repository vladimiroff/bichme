package cmd

import "fmt"

func minInt(name string, v, min int) error {
	if min > v {
		return fmt.Errorf("minimal value of '%v' is '%v'; got '%v'", name, min, v)
	}
	return nil
}

func maxInt(name string, v, max int) error {
	if max < v {
		return fmt.Errorf("maximum value of '%v' is '%v'; got '%v'", name, max, v)
	}
	return nil
}

func minLen(name string, v string, min int) error {
	if min > len(v) {
		return fmt.Errorf("minimal length of '%v' is '%v'; got '%v' (%d)", name, min, v, len(v))
	}
	return nil
}
