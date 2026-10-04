//go:build !darwin

package desktopcamera

func New() Capturer {
	return nil
}
