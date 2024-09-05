/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package k0scontext

import "context"

// keyType is used to create unique keys based on the types of the values stored in a context.
type keyType[T any] struct{}

// bucket is used to wrap values before storing them in a context.
type bucket[T any] struct{ inner T }

// Value retrieves the value of type T from the context.
// If there's no such value, it returns the zero value of type T.
func Value[T any](ctx context.Context) T {
	return ValueOrElse[T](ctx, func() (_ T) { return })
}

// ValueOrElse retrieves the value of type T from the context.
// If there's no such value, it invokes the fallback function and returns its result.
func ValueOrElse[T any](ctx context.Context, fallbackFn func() T) T {
	if val, ok := value[T](ctx); ok {
		return val.inner
	}

	return fallbackFn()
}

// value retrieves a value of type T from the context along with a boolean
// indicating its presence.
func value[T any](ctx context.Context) (bucket[T], bool) {
	var key keyType[T]
	val, ok := ctx.Value(key).(bucket[T])
	return val, ok
}
