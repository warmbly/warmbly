import { describe, expect, it } from "vitest";
import { buildPilotGate, PILOT_TARGET } from "./pilotGate";
import type {
  ConfengeReadiness,
  ConfengeSummary,
  ConfengeWorkingQueueSummary,
} from "@/lib/api/models/app/confenge/Confenge";

const readiness: ConfengeReadiness = {
  email: "ready",
  whatsapp: "not_configured",
  feed_configured: true,
  feed_age_seconds: 300,
  feed_age: "fresh",
  outcome_loop: "ready",
  ai: "ready",
  governor_cap: 10,
  campaign_daily_limit: 200,
  effective_daily_cap: 90,
  queue_count: 0,
  kill_switch: false,
  sending_allowed: true,
  outreach_enabled: true,
  require_human_approval: true,
  auto_send_enabled: false,
  whatsapp_enabled: false,
  pilot_cohort_state: "ready",
  pilot_cohort_prepared: 0,
  pilot_cohort_needs_review: 0,
  pilot_cohort_approved: 0,
  pilot_cohort_sent: 0,
};

const summary: ConfengeSummary = {
  needs_contact: 0,
  ready_to_generate: 0,
  needs_review: 0,
  approved: 0,
  enrolled: 0,
  sent: 0,
  replied: 0,
  meeting: 0,
  proposal: 0,
  won: 0,
  blocked: 0,
  bounced: 0,
  do_not_contact: 0,
  total: 439,
};

const overview: ConfengeWorkingQueueSummary = {
  reservoir_monitored: 439,
  actionable_now: 0,
  needs_contact: 0,
  needs_review: 0,
  approved_scheduled: 0,
  watch_awaiting: 271,
  suppressed: 0,
  stale_context: 0,
  due_next_24h: 0,
  theoretical_slots_24h: 90,
  capacity_load: 0,
  dynamic_priority_enabled: true,
};

describe("bloqueios do lote piloto", () => {
  it("usa o estado de frescor autoritativo retornado pelo backend", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, feed_state: "stale", feed_age_seconds: 60, feed_max_age_seconds: 86_400 },
      summary,
      overview,
      reviewCount: 0,
    });

    expect(gate.blockers[0]).toMatchObject({ id: "feed" });
  });

  it("expõe lote, aprovação e pausa quando nada foi preparado", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, kill_switch: true, sending_allowed: false },
      summary,
      overview,
      reviewCount: 0,
    });

    expect(gate.ready).toBe(false);
    expect(gate.blockers.map((blocker) => blocker.id)).toEqual(["cohort", "approval", "dispatch"]);
    expect(gate.blockers[0].detail).toContain("439 contas estão monitoradas");
    expect(gate.blockers[2]).toMatchObject({
      href: "#planejamento",
      action: "Ir para a primeira pendência",
    });
  });

  it("leva ao controle de envios quando a pausa é a única pendência", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, kill_switch: true, sending_allowed: false, pilot_cohort_prepared: PILOT_TARGET, pilot_cohort_approved: PILOT_TARGET },
      summary,
      overview,
      reviewCount: 0,
    });

    expect(gate.blockers).toEqual([
      expect.objectContaining({
        id: "dispatch",
        href: "#controle-envios",
        action: "Abrir controle de envios",
      }),
    ]);
  });

  it("libera o piloto quando trinta mensagens estão aprovadas e os canais estão prontos", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, pilot_cohort_prepared: PILOT_TARGET, pilot_cohort_approved: PILOT_TARGET },
      summary,
      overview,
      reviewCount: 0,
    });

    expect(gate.ready).toBe(true);
    expect(gate.blockers).toHaveLength(0);
  });

  it("não conta linhas DUE ou APPROVED da lista de revisão duas vezes", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, pilot_cohort_prepared: 7, pilot_cohort_needs_review: 4, pilot_cohort_approved: 3 },
      summary: { ...summary, needs_review: 4, approved: 3 },
      overview,
      reviewCount: 30,
    });

    expect(gate.prepared).toBe(7);
    expect(gate.blockers.map((blocker) => blocker.id)).toContain("cohort");
  });

  it("não usa estados globais quando a leitura persistente do cohort falha", () => {
    const gate = buildPilotGate({
      readiness: { ...readiness, pilot_cohort_state: "unavailable" },
      summary: { ...summary, approved: PILOT_TARGET, sent: PILOT_TARGET },
      overview,
      reviewCount: 0,
    });

    expect(gate.prepared).toBe(0);
    expect(gate.blockers[0]).toMatchObject({ id: "cohort", title: "Estado persistente do cohort indisponível" });
  });
});
