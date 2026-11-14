package flashpi

import (
	"bytes"
	"encoding/json"
)

// Nullable is an optional member of a request that may also be sent as null, which clears the
// field in an update (PATCH). Its zero value is not sent at all:
//
//	flashpi.UpdateIssuer{
//		TradeName: flashpi.Null[string](),              // clears the trade name
//		Branding: &flashpi.IssuerBrandingInput{
//			BrandColor: flashpi.NullableOf("#0C160D"), // sets the color
//		},
//		// Members left as they are keep their value.
//	}
type Nullable[T any] struct {
	value T
	state nullableState
}

type nullableState uint8

const (
	nullableUnset nullableState = iota
	nullableNull
	nullableSet
)

// Null is a Nullable sent as null.
func Null[T any]() Nullable[T] { return Nullable[T]{state: nullableNull} }

// NullableOf is a Nullable sent as value.
func NullableOf[T any](value T) Nullable[T] { return Nullable[T]{value: value, state: nullableSet} }

// IsZero reports whether n is unset, so a member tagged omitzero is left out.
func (n Nullable[T]) IsZero() bool { return n.state == nullableUnset }

// IsNull reports whether n is sent as null.
func (n Nullable[T]) IsNull() bool { return n.state == nullableNull }

// Value returns the value of n, and whether it has one.
func (n Nullable[T]) Value() (T, bool) { return n.value, n.state == nullableSet }

// MarshalJSON writes the value, or null when n is null or unset.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.state != nullableSet {
		return []byte("null"), nil
	}
	return marshalJSON(n.value)
}

// UnmarshalJSON reads null as Null and anything else as a value.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*n = Null[T]()
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*n = NullableOf(value)
	return nil
}
