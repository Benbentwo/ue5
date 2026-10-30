//go:build windows

package ci

import (
	"strings"
	"unicode/utf16"

	"github.com/danieljoos/wincred"
)

// credManagerSecret reads a generic credential named "ue5-ci:<name>" from the
// Windows Credential Manager of the user the daemon runs as. Store one with
//
//	cmdkey /generic:ue5-ci:azure-pat /user:ue5-ci /pass:<PAT>
//
// cmdkey stores the password as UTF-16; a blob written as UTF-8 also works.
func credManagerSecret(name string) (string, string) {
	target := "ue5-ci:" + name
	cred, err := wincred.GetGenericCredential(target)
	if err != nil || cred == nil || len(cred.CredentialBlob) == 0 {
		return "", ""
	}
	return strings.TrimSpace(decodeBlob(cred.CredentialBlob)), "Windows Credential Manager (" + target + ")"
}

func decodeBlob(b []byte) string {
	if len(b)%2 == 0 && len(b) >= 2 && b[1] == 0 {
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}
