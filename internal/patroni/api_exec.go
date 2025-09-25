package patroni

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/percona/percona-postgresql-operator/internal/logging"
)

// Executor implements API by calling "patronictl".
type Executor func(
	ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error

// Executor implements API.
var _ API = Executor(nil)

// ChangePrimaryAndWait tries to demote the current Patroni leader by calling
// "patronictl". It returns true when an election completes successfully. It
// waits up to two "loop_wait" or until an error occurs. When Patroni is paused,
// next cannot be blank. Similar to the "POST /switchover" REST endpoint.
func (exec Executor) ChangePrimaryAndWait(
	ctx context.Context, current, next string, patroniVer4 bool,
) (bool, error) {
	var stdout, stderr bytes.Buffer

	// K8SPG-648: patroni v4.0.0 deprecated "master" role.
	//            We should use "primary" instead
	cmd := []string{"patronictl", "switchover", "--scheduled=now", "--force", "--candidate=" + next}
	if patroniVer4 {
		cmd = append(cmd, "--primary="+current)
	} else {
		cmd = append(cmd, "--master="+current)
	}

	log := logging.FromContext(ctx)
	log.Info(
		"[PATRONI] Executing patronictl switchover command",
		"command",
		cmd,
		"current",
		current,
		"next",
		next,
		"patroniVer4",
		patroniVer4,
	)
	err := exec(ctx, nil, &stdout, &stderr, cmd...)

	log.V(1).Info("changed primary",
		"stdout", stdout.String(),
		"stderr", stderr.String(),
	)

	// The command exits zero when it is able to communicate with the Patroni
	// HTTP API. It exits zero even when the API says switchover did not occur.
	// Check for the text that indicates success.
	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/api.py#L351-L367
	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/api.py#L461-L477
	return strings.Contains(stdout.String(), "switched over"), err
}

// SwitchoverAndWait tries to change the current Patroni leader by calling
// "patronictl". It returns true when an election completes successfully. It
// waits up to two "loop_wait" or until an error occurs. When Patroni is paused,
// next cannot be blank. Similar to the "POST /switchover" REST endpoint.
// The "patronictl switchover" variant does not require the current master to be passed
// as a flag.
func (exec Executor) SwitchoverAndWait(
	ctx context.Context, target string,
) (bool, error) {
	var stdout, stderr bytes.Buffer

	log := logging.FromContext(ctx)
	cmd := []string{
		"patronictl",
		"switchover",
		"--scheduled=now",
		"--force",
		"--candidate=" + target,
	}
	log.Info(
		"[PATRONI] Executing patronictl switchover command (simplified)",
		"command",
		cmd,
		"target",
		target,
	)
	err := exec(ctx, nil, &stdout, &stderr, cmd...)

	log.V(1).Info("changed primary",
		"stdout", stdout.String(),
		"stderr", stderr.String(),
	)

	// The command exits zero when it is able to communicate with the Patroni
	// HTTP API. It exits zero even when the API says switchover did not occur.
	// Check for the text that indicates success.
	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/api.py#L351-L367
	// Patroni has an edge case where it could switchover to an instance other
	// than the requested candidate. In this case, stdout will contain
	// "Switched over" instead of "switched over" and return false, nil
	return strings.Contains(stdout.String(), "switched over"), err
}

// FailoverAndWait tries to change the current Patroni leader by calling
// "patronictl". It returns true when an election completes successfully. It
// waits up to two "loop_wait" or until an error occurs. When Patroni is paused,
// next cannot be blank. Similar to the "POST /switchover" REST endpoint.
// The "patronictl failover" variant does not require the current master to be passed
// as a flag.
func (exec Executor) FailoverAndWait(
	ctx context.Context, target string,
) (bool, error) {
	var stdout, stderr bytes.Buffer

	log := logging.FromContext(ctx)
	cmd := []string{"patronictl", "failover", "--force", "--candidate=" + target}
	log.Info("[PATRONI] Executing patronictl failover command", "command", cmd, "target", target)
	err := exec(ctx, nil, &stdout, &stderr, cmd...)

	log.V(1).Info("changed primary",
		"stdout", stdout.String(),
		"stderr", stderr.String(),
	)

	// The command exits zero when it is able to communicate with the Patroni
	// HTTP API. It exits zero even when the API says failover did not occur.
	// Check for the text that indicates success.
	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/api.py#L351-L367
	// Patroni has an edge case where it could failover to an instance other
	// than the requested candidate. In this case, stdout will contain "Failed over"
	// instead of "failed over" and return false, nil
	return strings.Contains(stdout.String(), "failed over"), err
}

// ReplaceConfiguration replaces Patroni's entire dynamic configuration by
// calling "patronictl". Similar to the "POST /switchover" REST endpoint.
func (exec Executor) ReplaceConfiguration(ctx context.Context, configuration map[string]any) error {
	var stdin, stdout, stderr bytes.Buffer

	err := json.NewEncoder(&stdin).Encode(configuration)
	if err == nil {
		log := logging.FromContext(ctx)
		cmd := []string{"patronictl", "edit-config", "--replace=-", "--force"}
		log.Info("[PATRONI] Executing patronictl edit-config command", "command", cmd)
		err = exec(ctx, &stdin, &stdout, &stderr, cmd...)

		log.V(1).Info("replaced configuration",
			"stdout", stdout.String(),
			"stderr", stderr.String(),
		)
	}

	return err
}

// RestartPendingMembers looks up Patroni members with role in scope and restarts
// those that have a pending restart.
func (exec Executor) RestartPendingMembers(ctx context.Context, role, scope string) error {
	var stdout, stderr bytes.Buffer

	// The following exits zero when it is able to read the DCS and communicate
	// with the Patroni HTTP API. It prints the result of calling "POST /restart"
	// on each member found with the desired role. The "Failed … 503 … restart
	// conditions are not satisfied" message is normal and means that a particular
	// member has already restarted.
	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/ctl.py#L580-L596
	log := logging.FromContext(ctx)
	cmd := []string{"patronictl", "restart", "--pending", "--force", "--role=" + role, scope}
	log.Info(
		"[PATRONI] Executing patronictl restart command",
		"command",
		cmd,
		"role",
		role,
		"scope",
		scope,
	)
	err := exec(ctx, nil, &stdout, &stderr, cmd...)

	log.V(1).Info("restarted members",
		"stdout", stdout.String(),
		"stderr", stderr.String(),
	)

	return err
}

// GetTimeline gets the patronictl status and returns the timeline,
// currently the only information required by PGO.
// Returns zero if it runs into errors or cannot find a running Leader pod
// to get the up-to-date timeline from.
func (exec Executor) GetTimeline(ctx context.Context) (int64, error) {
	var stdout, stderr bytes.Buffer

	// The following exits zero when it is able to read the DCS and communicate
	// with the Patroni HTTP API. It prints the result of calling "GET /cluster"
	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/ctl.py#L849
	log := logging.FromContext(ctx)
	cmd := []string{"patronictl", "list", "--format", "json"}
	log.Info("[PATRONI] Executing patronictl list command", "command", cmd)
	err := exec(ctx, nil, &stdout, &stderr, cmd...)
	if err != nil {
		return 0, err
	}

	if stderr.String() != "" {
		return 0, errors.New(stderr.String())
	}

	var members []struct {
		Role     string `json:"Role"`
		State    string `json:"State"`
		Timeline int64  `json:"TL"`
	}
	err = json.Unmarshal(stdout.Bytes(), &members)
	if err != nil {
		return 0, err
	}

	for _, member := range members {
		if member.Role == "Leader" && member.State == "running" {
			return member.Timeline, nil
		}
	}

	return 0, nil
}
