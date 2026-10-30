package panel

// HeaderCatalog is the set of ready-made header widgets.
func HeaderCatalog() []Panel {
	max := 100.0
	return []Panel{
		{ID: "waiting", Title: "Need you", Description: "Agents waiting on you and machine warnings in effect.", Viz: "stat", Source: "summary", Measure: "sum:need_you"},
		{ID: "finished", Title: "To review", Viz: "stat", Source: "summary", Measure: "sum:to_review"},
		{ID: "stuck", Title: "Stuck", Viz: "stat", Source: "summary", Measure: "sum:attention_stuck"},
		{ID: "leftover", Title: "Leftover", Viz: "stat", Source: "summary", Measure: "sum:leftover_bytes", Caption: "sum:leftover"},
		{ID: "reclaim", Title: "Reclaimable", Viz: "stat", Source: "cleanup", Measure: "sum:size_bytes", Where: []Filter{{Column: "status", Value: "ready"}}},
		{ID: "memory", Title: "Memory", Viz: "stat", Source: "local_activity", Measure: "max:mem_used_bytes", Total: "max:mem_total_bytes"},
		{ID: "cpu", Title: "CPU", Viz: "gauge", Source: "local_activity", Measure: "max:cpu_percent", Max: &max},
		{ID: "disk", Title: "Disk used", Viz: "stat", Source: "local_activity", Measure: "max:disk_used_bytes", Total: "max:disk_total_bytes"},
		{ID: "load", Title: "Load", Viz: "stat", Source: "local_activity", Measure: "max:load1"},
		{ID: "swap", Title: "Swap", Viz: "stat", Source: "local_activity", Measure: "max:swap_used_bytes"},
		{ID: "events", Title: "Events/min", Viz: "stat", Source: "local_activity", Measure: "max:events"},
		{ID: "agents", Title: "Agents", Viz: "stat", Source: "summary", Measure: "sum:agents"},
		{ID: "working", Title: "Working", Viz: "stat", Source: "summary", Measure: "sum:working"},
		{ID: "waiting_now", Title: "Waiting", Viz: "stat", Source: "summary", Measure: "sum:waiting"},
		{ID: "inbox", Title: "Inbox", Viz: "stat", Source: "summary", Measure: "sum:inbox"},
		{ID: "orphans", Title: "Leftover trees", Viz: "stat", Source: "summary", Measure: "sum:orphans"},
		{ID: "machines_up", Title: "Machines up", Viz: "stat", Source: "summary", Measure: "sum:machines_reachable", Total: "sum:machines"},
		{ID: "agent_memory", Title: "Agent memory", Viz: "stat", Source: "costs", Measure: "sum:mem_bytes"},
	}
}

// HeaderDefault lists the shipped header's widgets in display order.
func HeaderDefault() []string {
	return []string{"waiting", "finished", "stuck", "leftover", "reclaim", "memory"}
}
