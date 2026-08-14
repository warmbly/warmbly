import { describe, expect, it } from "vitest";
import { channelLabel, formatFeedAge, formatPtBrDate, intentLabel, purposeLabel, reasonLabel, stateLabel } from "./labels";

describe("rótulos da Central comercial CONFENGE", () => {
  it("traduz estados, canais, etapas e motivos conhecidos", () => {
    expect(stateLabel("NEEDS_REVIEW")).toBe("Precisa de revisão");
    expect(stateLabel("NEEDS_ENRICHMENT")).toBe("Precisa de enriquecimento");
    expect(stateLabel("READY")).toBe("Pronto");
    expect(stateLabel("VALIDATED")).toBe("Destinatário validado");
    expect(stateLabel("EXCEPTION")).toBe("Exceção de destinatário");
    expect(stateLabel("ROLE_MAILBOX_EXCEPTION")).toBe("Exceção de caixa funcional");
    expect(stateLabel("MANUAL_OUTREACH")).toBe("Abordagem manual");
    expect(stateLabel("ROUTED_CALL")).toBe("Ligação roteada");
    expect(stateLabel("GATEKEEPER_REACHED")).toBe("Falou com a recepção");
    expect(stateLabel("COPY_TEXT")).toBe("Copiar texto");
    expect(reasonLabel("generic_mailbox")).toContain("genérica");
    expect(reasonLabel("missing_contract_event")).toContain("evento contratual");
    expect(stateLabel("DO_NOT_CONTACT")).toBe("Não contatar");
    expect(channelLabel("EMAIL")).toBe("E-mail");
    expect(intentLabel("POSITIVE_INTEREST")).toBe("Interesse positivo");
    expect(purposeLabel("FOLLOW_UP")).toBe("Acompanhamento");
    expect(reasonLabel("unsupported_claim")).toBe("Afirmação sem evidência");
  });

  it("expõe reason codes desconhecidos de forma acionável", () => {
    expect(stateLabel("NEW_OPERATIONAL_STATE")).toBe("Estado não reconhecido");
    expect(reasonLabel("NEW_BLOCK_REASON")).toBe("Bloqueio operacional (NEW_BLOCK_REASON)");
    expect(reasonLabel("NEW_BLOCK_REASON")).not.toContain("Motivo não reconhecido");
  });

  it("formata datas no padrão brasileiro", () => {
    expect(formatPtBrDate("2026-08-12T12:00:00Z")).toBe("12/08/2026");
    expect(formatFeedAge(7200)).toBe("há 2 h");
  });
});
