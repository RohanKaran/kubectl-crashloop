package crashloop

import "time"

// DemoReport returns a deterministic sample report used by docs and demos.
func DemoReport() CrashReport {
	base := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)
	exit137 := 137
	exit1 := 1

	return CrashReport{
		PodName:     "payments-api-6d9c9b77d9-x2n5k",
		Namespace:   "production",
		ContextName: "demo-west",
		GeneratedAt: base.Add(8 * time.Minute),
		Warnings: []string{
			"Historical Events may have expired on this cluster; showing baseline pod termination state.",
		},
		Entries: []CrashEntry{
			{
				Container:     "api",
				Timestamp:     base.Add(7 * time.Minute),
				Reason:        "OOMKilled",
				ExitCode:      &exit137,
				Message:       "Container hit the memory limit during a bulk invoice import.",
				TailLogs:      "panic: runtime error: out of memory\nworker 4: allocating 134217728 bytes",
				TailLogSource: TailLogSourcePrevious,
				Source:        SourceLastTerminationState,
			},
			{
				Container: "api",
				Timestamp: base.Add(3 * time.Minute),
				Reason:    "BackOff",
				Message:   "Back-off restarting failed container api in pod payments-api-6d9c9b77d9-x2n5k_production",
				Source:    SourceEvent,
			},
			{
				Container:     "worker",
				Timestamp:     base.Add(6 * time.Minute),
				Reason:        "Error",
				ExitCode:      &exit1,
				Message:       "Worker exited after exhausting retries against the orders database.",
				TailLogs:      "dial tcp 10.20.4.18:5432: connect: connection refused\nretry budget exhausted",
				TailLogSource: TailLogSourcePrevious,
				Source:        SourceLastTerminationState,
			},
			{
				Timestamp:     base.Add(7*time.Minute - 2*time.Second),
				Reason:        "NodeMemoryPressure",
				Message:       "Node worker-node-1 experienced memory pressure: System OOM encountered, victim process: worker",
				Source:        SourceNodeEvent,
			},
		},
	}
}
