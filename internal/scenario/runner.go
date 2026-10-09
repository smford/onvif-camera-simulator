package scenario

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"gopkg.in/yaml.v3"
)

// Step defines an individual timed action in a test scenario playbook.
type Step struct {
	At              time.Duration `yaml:"at"`
	Action          string        `yaml:"action"` // "event" or "chaos"
	Camera          int           `yaml:"camera"` // camera index (0 -> cam0)
	Type            string        `yaml:"type"`   // "motion", "human", "vehicle", "freeze", "reboot", "loss"
	State           bool          `yaml:"state"`
	BBox            [4]float64    `yaml:"bbox"` // [x, y, w, h]
	Duration        time.Duration `yaml:"duration"`
	LossPercentage  int           `yaml:"loss_percentage"`
}

// Playbook represents the top-level YAML structure.
type Playbook struct {
	Scenario []Step `yaml:"scenario"`
}

// Runner executes automated scenario steps in sequence.
type Runner struct {
	path     string
	eventMgr *events.Manager
	chaosEng *chaos.Engine
}

// NewRunner creates a scenario playbook executor.
func NewRunner(path string, eventMgr *events.Manager, chaosEng *chaos.Engine) *Runner {
	return &Runner{
		path:     path,
		eventMgr: eventMgr,
		chaosEng: chaosEng,
	}
}

// Run executes the scenario playbook against the running system.
func (r *Runner) Run(ctx context.Context) error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return fmt.Errorf("failed to read scenario playbook %q: %w", r.path, err)
	}

	var pb Playbook
	if err := yaml.Unmarshal(data, &pb); err != nil {
		return fmt.Errorf("failed to parse scenario playbook %q: %w", r.path, err)
	}

	slog.Info("Starting CI/CD scenario playbook execution", "path", r.path, "steps", len(pb.Scenario))
	startTime := time.Now()

	for idx, step := range pb.Scenario {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		targetTime := startTime.Add(step.At)
		sleepDuration := time.Until(targetTime)
		if sleepDuration > 0 {
			time.Sleep(sleepDuration)
		}

		camStr := fmt.Sprintf("cam%d", step.Camera)
		slog.Info("Executing scenario step", "index", idx+1, "action", step.Action, "type", step.Type, "cam", camStr)

		switch step.Action {
		case "event":
			switch step.Type {
			case "motion":
				r.eventMgr.TriggerMotion(camStr, step.State, step.Duration)
			case "human":
				r.eventMgr.TriggerHuman(camStr, step.State, step.Duration, step.BBox)
			case "vehicle":
				r.eventMgr.TriggerVehicle(camStr, step.State, step.Duration, step.BBox)
			default:
				slog.Warn("Unknown event action type in scenario", "type", step.Type)
			}

		case "chaos":
			switch step.Type {
			case "freeze":
				r.chaosEng.TriggerFreeze(step.Duration)
			case "unfreeze":
				r.chaosEng.Unfreeze()
			case "reboot":
				r.chaosEng.TriggerReboot(step.Duration)
			case "loss", "packet_loss":
				r.chaosEng.SetPacketLoss(step.LossPercentage, step.Duration)
			default:
				slog.Warn("Unknown chaos action type in scenario", "type", step.Type)
			}

		default:
			slog.Warn("Unknown action in scenario", "action", step.Action)
		}
	}

	slog.Info("CI/CD scenario playbook execution completed successfully")
	return nil
}
