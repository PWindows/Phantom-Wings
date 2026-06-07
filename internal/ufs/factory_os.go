//go:build windows || darwin

package ufs

func NewSandboxFS(root string, _ bool) (*Quota, error) {
	osFS, err := NewOsFS(root)
	if err != nil {
		return nil, err
	}
	return NewQuota(osFS, 0), nil
}
