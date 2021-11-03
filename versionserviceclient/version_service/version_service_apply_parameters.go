// Code generated by go-swagger; DO NOT EDIT.

package version_service

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
)

// NewVersionServiceApplyParams creates a new VersionServiceApplyParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewVersionServiceApplyParams() *VersionServiceApplyParams {
	return &VersionServiceApplyParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewVersionServiceApplyParamsWithTimeout creates a new VersionServiceApplyParams object
// with the ability to set a timeout on a request.
func NewVersionServiceApplyParamsWithTimeout(timeout time.Duration) *VersionServiceApplyParams {
	return &VersionServiceApplyParams{
		timeout: timeout,
	}
}

// NewVersionServiceApplyParamsWithContext creates a new VersionServiceApplyParams object
// with the ability to set a context for a request.
func NewVersionServiceApplyParamsWithContext(ctx context.Context) *VersionServiceApplyParams {
	return &VersionServiceApplyParams{
		Context: ctx,
	}
}

// NewVersionServiceApplyParamsWithHTTPClient creates a new VersionServiceApplyParams object
// with the ability to set a custom HTTPClient for a request.
func NewVersionServiceApplyParamsWithHTTPClient(client *http.Client) *VersionServiceApplyParams {
	return &VersionServiceApplyParams{
		HTTPClient: client,
	}
}

/* VersionServiceApplyParams contains all the parameters to send to the API endpoint
   for the version service apply operation.

   Typically these are written to a http.Request.
*/
type VersionServiceApplyParams struct {

	// Apply.
	Apply string

	// BackupVersion.
	BackupVersion *string

	// CustomResourceUID.
	CustomResourceUID *string

	// DatabaseVersion.
	DatabaseVersion *string

	// HaproxyVersion.
	HaproxyVersion *string

	// KubeVersion.
	KubeVersion *string

	// LogCollectorVersion.
	LogCollectorVersion *string

	// NamespaceUID.
	NamespaceUID *string

	// OperatorVersion.
	OperatorVersion string

	// Platform.
	Platform *string

	// PmmVersion.
	PmmVersion *string

	// Product.
	Product string

	// ProxysqlVersion.
	ProxysqlVersion *string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the version service apply params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *VersionServiceApplyParams) WithDefaults() *VersionServiceApplyParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the version service apply params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *VersionServiceApplyParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the version service apply params
func (o *VersionServiceApplyParams) WithTimeout(timeout time.Duration) *VersionServiceApplyParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the version service apply params
func (o *VersionServiceApplyParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the version service apply params
func (o *VersionServiceApplyParams) WithContext(ctx context.Context) *VersionServiceApplyParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the version service apply params
func (o *VersionServiceApplyParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the version service apply params
func (o *VersionServiceApplyParams) WithHTTPClient(client *http.Client) *VersionServiceApplyParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the version service apply params
func (o *VersionServiceApplyParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithApply adds the apply to the version service apply params
func (o *VersionServiceApplyParams) WithApply(apply string) *VersionServiceApplyParams {
	o.SetApply(apply)
	return o
}

// SetApply adds the apply to the version service apply params
func (o *VersionServiceApplyParams) SetApply(apply string) {
	o.Apply = apply
}

// WithBackupVersion adds the backupVersion to the version service apply params
func (o *VersionServiceApplyParams) WithBackupVersion(backupVersion *string) *VersionServiceApplyParams {
	o.SetBackupVersion(backupVersion)
	return o
}

// SetBackupVersion adds the backupVersion to the version service apply params
func (o *VersionServiceApplyParams) SetBackupVersion(backupVersion *string) {
	o.BackupVersion = backupVersion
}

// WithCustomResourceUID adds the customResourceUID to the version service apply params
func (o *VersionServiceApplyParams) WithCustomResourceUID(customResourceUID *string) *VersionServiceApplyParams {
	o.SetCustomResourceUID(customResourceUID)
	return o
}

// SetCustomResourceUID adds the customResourceUid to the version service apply params
func (o *VersionServiceApplyParams) SetCustomResourceUID(customResourceUID *string) {
	o.CustomResourceUID = customResourceUID
}

// WithDatabaseVersion adds the databaseVersion to the version service apply params
func (o *VersionServiceApplyParams) WithDatabaseVersion(databaseVersion *string) *VersionServiceApplyParams {
	o.SetDatabaseVersion(databaseVersion)
	return o
}

// SetDatabaseVersion adds the databaseVersion to the version service apply params
func (o *VersionServiceApplyParams) SetDatabaseVersion(databaseVersion *string) {
	o.DatabaseVersion = databaseVersion
}

// WithHaproxyVersion adds the haproxyVersion to the version service apply params
func (o *VersionServiceApplyParams) WithHaproxyVersion(haproxyVersion *string) *VersionServiceApplyParams {
	o.SetHaproxyVersion(haproxyVersion)
	return o
}

// SetHaproxyVersion adds the haproxyVersion to the version service apply params
func (o *VersionServiceApplyParams) SetHaproxyVersion(haproxyVersion *string) {
	o.HaproxyVersion = haproxyVersion
}

// WithKubeVersion adds the kubeVersion to the version service apply params
func (o *VersionServiceApplyParams) WithKubeVersion(kubeVersion *string) *VersionServiceApplyParams {
	o.SetKubeVersion(kubeVersion)
	return o
}

// SetKubeVersion adds the kubeVersion to the version service apply params
func (o *VersionServiceApplyParams) SetKubeVersion(kubeVersion *string) {
	o.KubeVersion = kubeVersion
}

// WithLogCollectorVersion adds the logCollectorVersion to the version service apply params
func (o *VersionServiceApplyParams) WithLogCollectorVersion(logCollectorVersion *string) *VersionServiceApplyParams {
	o.SetLogCollectorVersion(logCollectorVersion)
	return o
}

// SetLogCollectorVersion adds the logCollectorVersion to the version service apply params
func (o *VersionServiceApplyParams) SetLogCollectorVersion(logCollectorVersion *string) {
	o.LogCollectorVersion = logCollectorVersion
}

// WithNamespaceUID adds the namespaceUID to the version service apply params
func (o *VersionServiceApplyParams) WithNamespaceUID(namespaceUID *string) *VersionServiceApplyParams {
	o.SetNamespaceUID(namespaceUID)
	return o
}

// SetNamespaceUID adds the namespaceUid to the version service apply params
func (o *VersionServiceApplyParams) SetNamespaceUID(namespaceUID *string) {
	o.NamespaceUID = namespaceUID
}

// WithOperatorVersion adds the operatorVersion to the version service apply params
func (o *VersionServiceApplyParams) WithOperatorVersion(operatorVersion string) *VersionServiceApplyParams {
	o.SetOperatorVersion(operatorVersion)
	return o
}

// SetOperatorVersion adds the operatorVersion to the version service apply params
func (o *VersionServiceApplyParams) SetOperatorVersion(operatorVersion string) {
	o.OperatorVersion = operatorVersion
}

// WithPlatform adds the platform to the version service apply params
func (o *VersionServiceApplyParams) WithPlatform(platform *string) *VersionServiceApplyParams {
	o.SetPlatform(platform)
	return o
}

// SetPlatform adds the platform to the version service apply params
func (o *VersionServiceApplyParams) SetPlatform(platform *string) {
	o.Platform = platform
}

// WithPmmVersion adds the pmmVersion to the version service apply params
func (o *VersionServiceApplyParams) WithPmmVersion(pmmVersion *string) *VersionServiceApplyParams {
	o.SetPmmVersion(pmmVersion)
	return o
}

// SetPmmVersion adds the pmmVersion to the version service apply params
func (o *VersionServiceApplyParams) SetPmmVersion(pmmVersion *string) {
	o.PmmVersion = pmmVersion
}

// WithProduct adds the product to the version service apply params
func (o *VersionServiceApplyParams) WithProduct(product string) *VersionServiceApplyParams {
	o.SetProduct(product)
	return o
}

// SetProduct adds the product to the version service apply params
func (o *VersionServiceApplyParams) SetProduct(product string) {
	o.Product = product
}

// WithProxysqlVersion adds the proxysqlVersion to the version service apply params
func (o *VersionServiceApplyParams) WithProxysqlVersion(proxysqlVersion *string) *VersionServiceApplyParams {
	o.SetProxysqlVersion(proxysqlVersion)
	return o
}

// SetProxysqlVersion adds the proxysqlVersion to the version service apply params
func (o *VersionServiceApplyParams) SetProxysqlVersion(proxysqlVersion *string) {
	o.ProxysqlVersion = proxysqlVersion
}

// WriteToRequest writes these params to a swagger request
func (o *VersionServiceApplyParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// path param apply
	if err := r.SetPathParam("apply", o.Apply); err != nil {
		return err
	}

	if o.BackupVersion != nil {

		// query param backupVersion
		var qrBackupVersion string

		if o.BackupVersion != nil {
			qrBackupVersion = *o.BackupVersion
		}
		qBackupVersion := qrBackupVersion
		if qBackupVersion != "" {

			if err := r.SetQueryParam("backupVersion", qBackupVersion); err != nil {
				return err
			}
		}
	}

	if o.CustomResourceUID != nil {

		// query param customResourceUid
		var qrCustomResourceUID string

		if o.CustomResourceUID != nil {
			qrCustomResourceUID = *o.CustomResourceUID
		}
		qCustomResourceUID := qrCustomResourceUID
		if qCustomResourceUID != "" {

			if err := r.SetQueryParam("customResourceUid", qCustomResourceUID); err != nil {
				return err
			}
		}
	}

	if o.DatabaseVersion != nil {

		// query param databaseVersion
		var qrDatabaseVersion string

		if o.DatabaseVersion != nil {
			qrDatabaseVersion = *o.DatabaseVersion
		}
		qDatabaseVersion := qrDatabaseVersion
		if qDatabaseVersion != "" {

			if err := r.SetQueryParam("databaseVersion", qDatabaseVersion); err != nil {
				return err
			}
		}
	}

	if o.HaproxyVersion != nil {

		// query param haproxyVersion
		var qrHaproxyVersion string

		if o.HaproxyVersion != nil {
			qrHaproxyVersion = *o.HaproxyVersion
		}
		qHaproxyVersion := qrHaproxyVersion
		if qHaproxyVersion != "" {

			if err := r.SetQueryParam("haproxyVersion", qHaproxyVersion); err != nil {
				return err
			}
		}
	}

	if o.KubeVersion != nil {

		// query param kubeVersion
		var qrKubeVersion string

		if o.KubeVersion != nil {
			qrKubeVersion = *o.KubeVersion
		}
		qKubeVersion := qrKubeVersion
		if qKubeVersion != "" {

			if err := r.SetQueryParam("kubeVersion", qKubeVersion); err != nil {
				return err
			}
		}
	}

	if o.LogCollectorVersion != nil {

		// query param logCollectorVersion
		var qrLogCollectorVersion string

		if o.LogCollectorVersion != nil {
			qrLogCollectorVersion = *o.LogCollectorVersion
		}
		qLogCollectorVersion := qrLogCollectorVersion
		if qLogCollectorVersion != "" {

			if err := r.SetQueryParam("logCollectorVersion", qLogCollectorVersion); err != nil {
				return err
			}
		}
	}

	if o.NamespaceUID != nil {

		// query param namespaceUid
		var qrNamespaceUID string

		if o.NamespaceUID != nil {
			qrNamespaceUID = *o.NamespaceUID
		}
		qNamespaceUID := qrNamespaceUID
		if qNamespaceUID != "" {

			if err := r.SetQueryParam("namespaceUid", qNamespaceUID); err != nil {
				return err
			}
		}
	}

	// path param operatorVersion
	if err := r.SetPathParam("operatorVersion", o.OperatorVersion); err != nil {
		return err
	}

	if o.Platform != nil {

		// query param platform
		var qrPlatform string

		if o.Platform != nil {
			qrPlatform = *o.Platform
		}
		qPlatform := qrPlatform
		if qPlatform != "" {

			if err := r.SetQueryParam("platform", qPlatform); err != nil {
				return err
			}
		}
	}

	if o.PmmVersion != nil {

		// query param pmmVersion
		var qrPmmVersion string

		if o.PmmVersion != nil {
			qrPmmVersion = *o.PmmVersion
		}
		qPmmVersion := qrPmmVersion
		if qPmmVersion != "" {

			if err := r.SetQueryParam("pmmVersion", qPmmVersion); err != nil {
				return err
			}
		}
	}

	// path param product
	if err := r.SetPathParam("product", o.Product); err != nil {
		return err
	}

	if o.ProxysqlVersion != nil {

		// query param proxysqlVersion
		var qrProxysqlVersion string

		if o.ProxysqlVersion != nil {
			qrProxysqlVersion = *o.ProxysqlVersion
		}
		qProxysqlVersion := qrProxysqlVersion
		if qProxysqlVersion != "" {

			if err := r.SetQueryParam("proxysqlVersion", qProxysqlVersion); err != nil {
				return err
			}
		}
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
