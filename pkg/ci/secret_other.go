//go:build !windows

package ci

// credManagerSecret is the Windows Credential Manager lookup; elsewhere it finds nothing.
func credManagerSecret(string) (string, string) { return "", "" }
