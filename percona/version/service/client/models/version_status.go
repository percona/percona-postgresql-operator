// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
)

// VersionStatus Status describes the current version status.
//
// swagger:model versionStatus
type VersionStatus string

const (

	// VersionStatusStatusInvalid captures enum value "status_invalid"
	VersionStatusStatusInvalid VersionStatus = "status_invalid"

	// VersionStatusRecommended captures enum value "recommended"
	VersionStatusRecommended VersionStatus = "recommended"

	// VersionStatusAvailable captures enum value "available"
	VersionStatusAvailable VersionStatus = "available"

	// VersionStatusRequired captures enum value "required"
	VersionStatusRequired VersionStatus = "required"

	// VersionStatusDisabled captures enum value "disabled"
	VersionStatusDisabled VersionStatus = "disabled"
)

// for schema
var versionStatusEnum []interface{}

func init() {
	var res []VersionStatus
	if err := json.Unmarshal([]byte(`["status_invalid","recommended","available","required","disabled"]`), &res); err != nil {
		panic(err)
	}
	for _, v := range res {
		versionStatusEnum = append(versionStatusEnum, v)
	}
}

func (m VersionStatus) validateVersionStatusEnum(path, location string, value VersionStatus) error {
	if err := validate.EnumCase(path, location, value, versionStatusEnum, true); err != nil {
		return err
	}
	return nil
}

// Validate validates this version status
func (m VersionStatus) Validate(formats strfmt.Registry) error {
	var res []error

	// value enum
	if err := m.validateVersionStatusEnum("", "body", m); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
