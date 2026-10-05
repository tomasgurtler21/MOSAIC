package app_test

// Harness-specific usage shapes for the Runner-shape coverage tests.
// Token arrays are [input, cache read, cache creation, output].

// claudeCodeShape: usage_record events plus model/token_usage on
// invocation_end and on assistant turns. Some records are observed twice, as
// when a hook re-emits them at a later firing.
func claudeCodeShape() shapeSpec {
	const h, m = "claude-code", "model-cc"
	rec := func(id, agent string, u [4]int64) map[string]any {
		f := map[string]any{"record_id": id, "model": m, "token_usage": tokenUsage(u)}
		if agent != "" {
			f["agent_instance_id"] = agent
			f["source"] = "agent_transcript"
		} else {
			f["source"] = "orchestrator_transcript"
		}
		return shapeEvent(h, "usage_record", f)
	}
	turn := func(u [4]int64) map[string]any {
		return shapeEvent(h, "turn", map[string]any{"role": "assistant", "model": m, "token_usage": tokenUsage(u)})
	}
	end := func(agent string, u [4]int64) map[string]any {
		return shapeEvent(h, "invocation_end", map[string]any{
			"agent_instance_id": agent, "status_code": "SUCCESS", "model": m, "token_usage": tokenUsage(u),
		})
	}
	start := func(agent, typ string) map[string]any {
		return shapeEvent(h, "invocation_start", map[string]any{"agent_instance_id": agent, "agent_type": typ})
	}
	o1, o2 := [4]int64{1000, 0, 0, 200}, [4]int64{2000, 500, 0, 400}
	a1, a2 := [4]int64{10000, 0, 0, 2000}, [4]int64{20000, 0, 4000, 3000}
	p1 := [4]int64{7000, 1000, 0, 900}
	return shapeSpec{
		harness: h, model: m,
		orchCalls: [2][]map[string]any{
			{rec("o-1", "", o1), turn(o1), rec("o-1", "", o1)},
			{rec("o-2", "", o2), turn(o2), rec("o-2", "", o2)},
		},
		wantOrch: [4]int64{3000, 500, 0, 600},
		agents: []shapeAgent{
			{
				id: "Research#1", want: [4]int64{30000, 0, 4000, 5000},
				events: []map[string]any{
					start("Research#1", "Research"),
					rec("a-1", "Research#1", a1), rec("a-2", "Research#1", a2), rec("a-1", "Research#1", a1),
					turn([4]int64{20000, 0, 4000, 3000}),
					end("Research#1", [4]int64{30000, 0, 4000, 5000}),
				},
			},
			{
				id: "Plan#1", want: p1,
				events: []map[string]any{
					start("Plan#1", "Plan"), rec("p-1", "Plan#1", p1), turn(p1), end("Plan#1", p1),
				},
			},
		},
	}
}

// ghcpCLIShape: no usage_record at all; usage only via model/token_usage on
// invocation_end and assistant turns.
func ghcpCLIShape() shapeSpec {
	const h, m = "ghcp-cli", "model-gh"
	turn := func(role string, u *[4]int64) map[string]any {
		f := map[string]any{"role": role}
		if u != nil {
			f["model"] = m
			f["token_usage"] = tokenUsage(*u)
		}
		return shapeEvent(h, "turn", f)
	}
	end := func(agent string, u [4]int64) map[string]any {
		return shapeEvent(h, "invocation_end", map[string]any{
			"agent_instance_id": agent, "status_code": "SUCCESS", "model": m, "token_usage": tokenUsage(u),
		})
	}
	start := func(agent, typ string) map[string]any {
		return shapeEvent(h, "invocation_start", map[string]any{"agent_instance_id": agent, "agent_type": typ})
	}
	o1, o2 := [4]int64{4000, 1000, 0, 800}, [4]int64{6000, 2000, 0, 1200}
	r1, p1, r2 := [4]int64{12000, 0, 0, 1500}, [4]int64{5000, 500, 0, 700}, [4]int64{800, 0, 0, 100}
	return shapeSpec{
		harness: h, model: m,
		orchCalls: [2][]map[string]any{
			{turn("user", nil), turn("assistant", &o1)},
			{turn("user", nil), turn("assistant", &o2)},
		},
		wantOrch: [4]int64{10000, 3000, 0, 2000},
		agents: []shapeAgent{
			{id: "Research#1", want: r1, events: []map[string]any{
				start("Research#1", "Research"), turn("assistant", &r1), end("Research#1", r1)}},
			{id: "Plan#1", want: p1, events: []map[string]any{
				start("Plan#1", "Plan"), turn("assistant", &p1), end("Plan#1", p1)}},
			{id: "Research#2", want: r2, events: []map[string]any{
				start("Research#2", "Research"), end("Research#2", r2)}},
		},
	}
}

// openCodeShape: usage only via usage_record events; invocation_end carries
// agent_instance_id, status_code and response only.
func openCodeShape() shapeSpec {
	const h, m = "opencode", "model-oc"
	rec := func(id, agent string, u [4]int64) map[string]any {
		f := map[string]any{"record_id": id, "model": m, "token_usage": tokenUsage(u)}
		if agent != "" {
			f["agent_instance_id"] = agent
		}
		return shapeEvent(h, "usage_record", f)
	}
	end := func(agent string) map[string]any {
		return shapeEvent(h, "invocation_end", map[string]any{
			"agent_instance_id": agent, "status_code": "SUCCESS", "response": "done",
		})
	}
	start := func(agent, typ string) map[string]any {
		return shapeEvent(h, "invocation_start", map[string]any{"agent_instance_id": agent, "agent_type": typ})
	}
	asstTurn := shapeEvent(h, "turn", map[string]any{"role": "assistant", "model": m})
	o1, o2 := [4]int64{2500, 300, 0, 500}, [4]int64{3500, 0, 0, 700}
	a1, a2, p1 := [4]int64{8000, 0, 0, 1000}, [4]int64{9000, 0, 2000, 1500}, [4]int64{3000, 0, 0, 400}
	return shapeSpec{
		harness: h, model: m,
		orchCalls: [2][]map[string]any{
			{asstTurn, rec("o-1", "", o1)},
			{asstTurn, rec("o-2", "", o2), rec("o-2", "", o2)},
		},
		wantOrch: [4]int64{6000, 300, 0, 1200},
		agents: []shapeAgent{
			{id: "Research#1", want: [4]int64{17000, 0, 2000, 2500}, events: []map[string]any{
				start("Research#1", "Research"),
				rec("a-1", "Research#1", a1), rec("a-2", "Research#1", a2), rec("a-2", "Research#1", a2),
				end("Research#1")}},
			{id: "Plan#1", want: p1, events: []map[string]any{
				start("Plan#1", "Plan"), rec("p-1", "Plan#1", p1), end("Plan#1")}},
		},
	}
}
