package domain

type RuntimeCredential struct {
	Access
	CredentialVersionRef string
}

func (r RuntimeCredential) Validate() error {
	if r.Access.Validate() != nil || !validID(r.CredentialVersionRef) {
		return ErrInvalidAccess
	}
	return nil
}
