//go:build unix

package ufs

func NewSandboxFS(root string, useOpenat2 bool) (*Quota, error) {
	unixFS, err := NewUnixFS(root, useOpenat2)
	if err != nil {
		return nil, err
	}
	return NewQuota(unixFS, 0), nil
}
