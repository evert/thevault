//go:build !unix

package main

func setUmask() {}

func writable(string) bool { return true }
