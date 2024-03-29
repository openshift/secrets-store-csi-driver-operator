// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

// PublicKeyApplyConfiguration represents an declarative configuration of the PublicKey type for use
// with apply.
type PublicKeyApplyConfiguration struct {
	KeyData      *string `json:"keyData,omitempty"`
	RekorKeyData *string `json:"rekorKeyData,omitempty"`
}

// PublicKeyApplyConfiguration constructs an declarative configuration of the PublicKey type for use with
// apply.
func PublicKey() *PublicKeyApplyConfiguration {
	return &PublicKeyApplyConfiguration{}
}

// WithKeyData sets the KeyData field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the KeyData field is set to the value of the last call.
func (b *PublicKeyApplyConfiguration) WithKeyData(value string) *PublicKeyApplyConfiguration {
	b.KeyData = &value
	return b
}

// WithRekorKeyData sets the RekorKeyData field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the RekorKeyData field is set to the value of the last call.
func (b *PublicKeyApplyConfiguration) WithRekorKeyData(value string) *PublicKeyApplyConfiguration {
	b.RekorKeyData = &value
	return b
}
