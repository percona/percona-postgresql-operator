// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package patroni

import (
	"context"
)

// API defines a general interface for interacting with the Patroni API.
type API interface {
	// ChangePrimaryAndWait tries to demote the current Patroni leader. It
	// returns true when an election completes successfully. When Patroni is
	// paused, next cannot be blank.
	ChangePrimaryAndWait(ctx context.Context, current, next string, patroniVer4 bool) (bool, error)

	// ReplaceConfiguration replaces Patroni's entire dynamic configuration.
	ReplaceConfiguration(ctx context.Context, configuration map[string]any) error

	// SwitchoverAndWait tries to change the current Patroni leader. It
	// returns true when an election completes successfully. When Patroni is
	// paused, next cannot be blank.
	SwitchoverAndWait(ctx context.Context, target string) (bool, error)

	// FailoverAndWait tries to change the current Patroni leader. It
	// returns true when an election completes successfully. When Patroni is
	// paused, next cannot be blank.
	FailoverAndWait(ctx context.Context, target string) (bool, error)

	// RestartPendingMembers looks up Patroni members with role in scope and
	// restarts those that have a pending restart.
	RestartPendingMembers(ctx context.Context, role, scope string) error

	// GetTimeline gets the current timeline from Patroni cluster status.
	// Returns zero if it runs into errors or cannot find a running Leader pod.
	GetTimeline(ctx context.Context) (int64, error)
}
