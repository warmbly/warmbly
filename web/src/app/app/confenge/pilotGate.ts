import type {
  ConfengeReadiness,
  ConfengeSummary,
  ConfengeWorkingQueueSummary,
} from "@/lib/api/models/app/confenge/Confenge";

export const PILOT_TARGET = 30;

export type PilotBlocker = {
  id: "email" | "feed" | "outcome" | "stale" | "cohort" | "approval" | "dispatch";
  title: string;
  detail: string;
  href: string;
  action: string;
};

export type PilotGate = {
  evaluating: boolean;
  ready: boolean;
  prepared: number;
  released: number;
  target: number;
  blockers: PilotBlocker[];
};

export function buildPilotGate(input: {
  readiness?: ConfengeReadiness;
  summary?: ConfengeSummary;
  overview?: ConfengeWorkingQueueSummary;
  reviewCount?: number;
}): PilotGate {
  const { readiness, summary, overview, reviewCount } = input;
  if (!readiness || !summary || !overview || reviewCount === undefined) {
    return { evaluating: true, ready: false, prepared: 0, released: 0, target: PILOT_TARGET, blockers: [] };
  }

  const cohortAvailable = readiness.pilot_cohort_state === "ready";
  const released = cohortAvailable
    ? (readiness.pilot_cohort_approved ?? 0) + (readiness.pilot_cohort_sent ?? 0)
    : 0;
  const needsReview = cohortAvailable ? (readiness.pilot_cohort_needs_review ?? 0) : 0;
  const prepared = cohortAvailable ? (readiness.pilot_cohort_prepared ?? 0) : 0;
  const blockers: PilotBlocker[] = [];

  if (readiness.email !== "ready") {
    blockers.push({
      id: "email",
      title: "Caixa de e-mail indisponível",
      detail: "O piloto não pode começar sem uma caixa ativa e apta a enviar.",
      href: "#visao-geral",
      action: "Ver condições",
    });
  }

  const feedMaxAge = readiness.feed_max_age_seconds ?? 86_400;
  const feedBlocked = readiness.feed_state
    ? readiness.feed_state !== "fresh"
    : readiness.feed_age_seconds === null || readiness.feed_age_seconds === undefined || readiness.feed_age_seconds > feedMaxAge;
  if (!readiness.feed_configured || feedBlocked) {
    blockers.push({
      id: "feed",
      title: "Base comercial ausente ou desatualizada",
      detail: "Atualize o feed antes de escolher as empresas do lote piloto.",
      href: "#visao-geral",
      action: "Ver condições",
    });
  }

  if (readiness.outcome_loop !== "ready") {
    blockers.push({
      id: "outcome",
      title: "Retorno de resultados não está pronto",
      detail: "O ciclo de resultados precisa estar ativo antes do primeiro envio real.",
      href: "#visao-geral",
      action: "Ver condições",
    });
  }

  if (overview.stale_context > 0) {
    blockers.push({
      id: "stale",
      title: `${overview.stale_context} ${overview.stale_context === 1 ? "conta está" : "contas estão"} com contexto desatualizado`,
      detail: "Regere e reavalie essas mensagens antes de qualquer aprovação.",
      href: "#agir-agora",
      action: "Revisar contexto",
    });
  }

  if (!cohortAvailable || prepared < PILOT_TARGET) {
    const available = overview.actionable_now + summary.ready_to_generate;
    blockers.push({
      id: "cohort",
      title: cohortAvailable
        ? `Cohort manual incompleto: ${prepared} de ${PILOT_TARGET} mensagens preparadas`
        : "Estado persistente do cohort indisponível",
      detail: !cohortAvailable
        ? "Não prossiga com aprovação ou envio até o backend validar as memberships e mensagens exatas do piloto."
        : available > 0
        ? `Escolha manualmente entre ${available} ${available === 1 ? "conta elegível" : "contas elegíveis"} para completar o cohort.`
        : `${overview.reservoir_monitored} contas estão monitoradas, mas nenhuma passou pelos gates de target-fit, contato e prontidão de envio.`,
      href: "#planejamento",
      action: "Montar cohort",
    });
  }

  if (released < PILOT_TARGET) {
    blockers.push({
      id: "approval",
      title: `Aprovação manual incompleta: ${released} de ${PILOT_TARGET} mensagens liberadas`,
      detail: needsReview > 0
        ? `${needsReview} ${needsReview === 1 ? "mensagem aguarda" : "mensagens aguardam"} sua decisão individual.`
        : "Ainda não há mensagens na fila de revisão. Monte o cohort antes de aprovar.",
      href: needsReview > 0 ? "#revisao" : "#planejamento",
      action: needsReview > 0 ? "Revisar agora" : "Ir ao planejamento",
    });
  }

  if (readiness.kill_switch || !readiness.sending_allowed) {
    const firstPreviousBlocker = blockers[0];
    blockers.push({
      id: "dispatch",
      title: "Envios bloqueados pelo controle de segurança",
      detail: "Mantenha a pausa até concluir todas as aprovações. Retome somente como última etapa.",
      href: firstPreviousBlocker?.href ?? "#controle-envios",
      action: firstPreviousBlocker ? "Ir para a primeira pendência" : "Abrir controle de envios",
    });
  }

  return {
    evaluating: false,
    ready: blockers.length === 0,
    prepared,
    released,
    target: PILOT_TARGET,
    blockers,
  };
}
