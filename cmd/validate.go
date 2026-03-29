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
