const STATE_LABELS: Record<string, string> = {
  NEEDS_CONTACT: "Precisa de contato",
  READY_TO_GENERATE: "Pronta para gerar",
  NEEDS_REVIEW: "Precisa de revisão",
  APPROVED: "Aprovada",
  ENROLLED: "Incluída na campanha",
  SENT: "Enviada",
  REPLIED: "Respondida",
  MEETING: "Reunião",
  PROPOSAL: "Proposta",
  WON: "Ganha",
  LOST: "Perdida",
  BLOCKED: "Bloqueada",
  BOUNCED: "E-mail devolvido",
  DO_NOT_CONTACT: "Não contatar",
  SKIPPED: "Ignorada",
  TARGET_FIT_SUPPRESSED: "Fora do perfil",
  NOT_GENERATED: "Não gerada",
  GENERATING: "Gerando",
  REJECTED: "Rejeitada",
  PLANNED: "Planejada",
  DUE: "Pronta para executar",
  DRAFTED: "Rascunho criado",
  QUEUED: "Na fila de envio",
  DNC: "Não contatar",
  CANCELLED: "Cancelada",
  FAILED: "Falhou",
  READY: "Pronto",
  VALIDATED: "Destinatário validado",
  EXCEPTION: "Exceção de destinatário",
  DIRECT_EMAIL_VALIDATED: "E-mail direto validado",
  NAMED_HUMAN_MANUAL_CHANNEL: "Pessoa nomeada, canal manual",
  ROLE_MAILBOX_VALIDATED: "Caixa funcional oficial",
  GENERIC_CORPORATE_CONTACT: "Contato corporativo genérico",
  EXHAUSTED: "Sem ação automática",
  MANUAL_OUTREACH: "Abordagem manual",
  ROLE_MAILBOX_EXCEPTION: "Exceção de caixa funcional",
  LOW_CONFIDENCE_MANUAL: "Manual de baixa confiança",
  EMAIL_NEEDS_REVIEW: "E-mail para revisar",
  HUMAN_REVIEW_EMAIL: "E-mail inferido para revisar",
  CALL_QUEUE: "Fila de ligações",
  ROUTED_CALL_QUEUE: "Fila de ligação roteada",
  WHATSAPP_QUEUE: "Fila de WhatsApp",
  PROFESSIONAL_SOCIAL_QUEUE: "Fila de perfil profissional",
  ROLE_EMAIL_QUEUE: "Fila de caixa funcional",
  CONTACT_FORM_QUEUE: "Fila de formulário",
  DIRECT_EMAIL: "E-mail direto",
  INFERRED_EMAIL_REVIEW: "E-mail inferido",
  ROLE_EMAIL: "E-mail da função",
  GENERIC_EMAIL: "E-mail genérico",
  DIRECT_CALL: "Ligação direta",
  ROUTED_CALL: "Ligação roteada",
  PROFESSIONAL_SOCIAL: "Perfil profissional",
  CONTACT_FORM: "Formulário",
  OTHER_MANUAL: "Outra ação manual",
  IN_PROGRESS: "Em andamento",
  COMPLETED: "Concluída",
  NEEDS_FOLLOWUP: "Precisa de follow-up",
  GATEKEEPER_REACHED: "Falou com a recepção",
  REFERRED_TO_OTHER_PERSON: "Indicou outra pessoa",
  TARGET_REACHED: "Falou com o alvo",
  CALLBACK_REQUESTED: "Pediu retorno",
  NOT_INTERESTED: "Sem interesse",
  INTERESTED: "Interessado",
  MEETING_SCHEDULED: "Reunião marcada",
  NO_ANSWER: "Não atendeu",
  BUSY: "Ocupado",
  WRONG_PERSON: "Pessoa errada",
  WRONG_CHANNEL: "Canal errado",
  INVALID_ROUTE: "Rota inválida",
  INVALID_CHANNEL: "Canal inválido",
  FORM_SUBMITTED: "Formulário enviado",
  SOCIAL_MESSAGE_SENT: "Mensagem enviada",
  R1_DIRECT: "R1 direto",
  R2_HIGH_CONFIDENCE_DIRECT: "R2 inferido",
  R3_ROUTED_TO_NAMED_PERSON: "R3 roteado",
  R4_ROLE_ROUTE: "R4 função",
  R5_CORPORATE_ONLY: "R5 corporativo",
  R0_NO_ACTIONABLE_ROUTE: "R0 sem rota",
  COPY_TEXT: "Copiar texto",
  ATTEMPTED: "Tentativa registrada",
  CONTACTED: "Contatado",
  FOLLOW_UP: "Follow-up",
  MANUAL_ROUTED_CALL: "Ligação roteada",
  MARK_CONTACTED: "Marcar como contatado",
  CORRECT_CONTACT: "Corrigir contato",
  PROMOTE_AFTER_NEW_EVIDENCE: "Promover após nova evidência",
  NEEDS_ENRICHMENT: "Precisa de enriquecimento",
  NOT_READY: "Não está pronto",
  BLOCKED_BY_POLICY: "Bloqueado pela política",
  NOT_CONFIGURED: "Não configurado",
  FALLBACK_TEMPLATE: "Modelo alternativo",
};

const PURPOSE_LABELS: Record<string, string> = {
  INITIAL: "Contato inicial",
  FOLLOW_UP: "Acompanhamento",
  CLOSE: "Encerramento",
};

const CHANNEL_LABELS: Record<string, string> = {
  EMAIL: "E-mail",
  WHATSAPP: "WhatsApp",
  PHONE: "Telefone",
  LINKEDIN: "Perfil profissional",
  FORM: "Formulário",
};

const INTENT_LABELS: Record<string, string> = {
  POSITIVE_INTEREST: "Interesse positivo",
  REFERRAL_TO_OTHER_PERSON: "Encaminhamento para outra pessoa",
  QUESTION: "Pergunta",
  OBJECTION: "Objeção",
  NOT_NOW: "Retomar mais tarde",
  NEGATIVE: "Sem interesse",
  DO_NOT_CONTACT: "Não contatar",
  OUT_OF_OFFICE: "Ausência temporária",
  UNKNOWN: "Intenção ainda não classificada",
  NEW: "Nova resposta",
};

const REASON_LABELS: Record<string, string> = {
  factual_error: "Erro factual",
  too_generic: "Muito genérica",
  too_salesy: "Comercial demais",
  wrong_offer: "Oferta incorreta",
  unsupported_claim: "Afirmação sem evidência",
  creepy: "Abordagem invasiva",
  too_long: "Muito longa",
  tone: "Tom inadequado",
  other: "Outro motivo",
  manual_pause: "Pausa manual",
  empty_followup: "Acompanhamento sem conteúdo útil",
  banned_phrase: "Expressão não permitida",
  meeting_default_cta: "Pedido de reunião cedo demais",
  multiple_ctas: "Mais de uma chamada para ação",
  fake_re_fwd: "Assunto simula resposta ou encaminhamento",
  missing_why_now: "Falta explicar por que agir agora",
  offer_cannot_be_fulfilled: "Oferta não pode ser cumprida",
  evidence_weak: "Evidência insuficiente",
  missing_contract_event: "Existe contrato público, mas nenhum evento contratual específico sustenta ainda uma primeira abordagem",
  metadata_dump: "O fato público chegou como metadado, não como gancho comercial",
  no_safe_hook: "Não há fato público específico o bastante para um primeiro contato",
  missing_value_unit: "Não há unidade de valor concreta para oferecer",
  unfulfillable_cta: "O convite promete um conteúdo que o dossiê ainda não sustenta",
  reasoning_leak: "Raciocínio interno apareceu na mensagem",
  vocab_mismatch: "Vocabulário incompatível com o serviço",
  messageability_needs_enrichment: "Ainda não dá para um primeiro contato digno",
  messageability_blocked: "Abordagem bloqueada",
  composer_version_stale: "Rascunho anterior à correção de messageability",
  hypothesis_as_fact: "Hipótese apresentada como fato",
  short_email: "Mensagem curta demais",
  long_email: "Mensagem longa demais",
  generic_opening: "Abertura genérica",
  WINDOW_OPEN: "Janela de ação aberta",
  NEW_AMENDMENT_OR_TERM: "Novo aditivo ou encerramento",
  NEW_RELEVANT_CONTRACT: "Novo contrato relevante",
  generic_mailbox: "Caixa genérica: não prova uma pessoa. Não promover automaticamente.",
  generic_mailbox_allowed_by_policy: "Caixa genérica: confirme destinatário e saudação antes de aprovar",
  name_unproven: "Nome da pessoa não comprovado na evidência",
  role_unproven: "Função da pessoa não comprovada na evidência",
  suitability_ambiguous: "Adequação comercial do destinatário é ambígua",
  recipient_exception: "Exceção de destinatário: o humano precisa decidir a identidade",
  enrichment_unavailable: "Enriquecimento automático indisponível",
  recipient_conflict: "Mais de um destinatário válido no snapshot atual",
  recipient_removed_current_snapshot: "Contato removido do snapshot atual",
  recipient_changed_requires_review: "A cadência existente aponta para outro destinatário",
  recipient_snapshot_missing: "Snapshot autoritativo do contato ausente",
  account_not_in_current_snapshot: "Conta ausente do snapshot atual",
  cohort_membership_conflict: "Conta já preparada com outra mensagem ou destinatário",
  cohort_membership_failed: "Dependências da preparação ficaram incoerentes",
};

export function stateLabel(value?: string | null): string {
  if (!value) return "Não informado";
  return STATE_LABELS[value.toUpperCase()] ?? "Estado não reconhecido";
}

export function purposeLabel(value?: string | null): string {
  if (!value) return "Etapa";
  return PURPOSE_LABELS[value.toUpperCase()] ?? "Etapa não reconhecida";
}

export function channelLabel(value?: string | null): string {
  if (!value) return "Canal não informado";
  return CHANNEL_LABELS[value.toUpperCase()] ?? "Canal não reconhecido";
}

export function intentLabel(value?: string | null): string {
  if (!value) return "Intenção não informada";
  return INTENT_LABELS[value.toUpperCase()] ?? "Intenção não reconhecida";
}

export function reasonLabel(value?: string | null): string {
  if (!value) return "Não informado";
  return REASON_LABELS[value] ?? `Bloqueio operacional (${value})`;
}

export function formatPtBrDate(value: string | Date | null | undefined): string | null {
  if (!value) return null;
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat("pt-BR", { dateStyle: "short" }).format(date);
}

export function formatFeedAge(seconds?: number | null): string {
  if (seconds === null || seconds === undefined) return "horário desconhecido";
  if (seconds < 60) return "agora";
  if (seconds < 3600) return `há ${Math.floor(seconds / 60)} min`;
  if (seconds < 86400) return `há ${Math.floor(seconds / 3600)} h`;
  const days = Math.floor(seconds / 86400);
  return `há ${days} ${days === 1 ? "dia" : "dias"}`;
}
