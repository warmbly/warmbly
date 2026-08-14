import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import {
  Building2,
  ChartNoAxesColumnIncreasing,
  CircleAlert,
  ContactRound,
  LayoutDashboard,
  ListChecks,
  Loader2,
  type LucideIcon,
  Menu,
  ShieldAlert,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import SocketProvider from "@/hooks/SocketProvider";
import ConfirmProvider from "@/hooks/ConfirmProvider";
import { useSocket } from "@/hooks/context/socket";
import Request from "@/lib/api/client/Request";
import { ensureConfengeOperatorSession } from "@/lib/api/client/auth/confengeOperatorSession";
import type { AppError } from "@/lib/api/client/normalizeError";
import {
  useConfengeReviewTouchpoints,
  useConfengeStatus,
  useConfengeSummary,
  useConfengeWorkingOverview,
} from "@/lib/api/hooks/app/confenge/useConfenge";
import { buildPilotGate } from "./pilotGate";

type OperatorIdentity = {
  user_id: string;
  organization_id?: string;
  organization_name?: string;
};

type OperatorNavItem = {
  href: string;
  label: string;
  description: string;
  icon: LucideIcon;
};

const OPERATOR_NAV: OperatorNavItem[] = [
  { href: "#pendencias-criticas", label: "Pendências críticas", description: "Bloqueios para o piloto", icon: ShieldAlert },
  { href: "#visao-geral", label: "Visão geral", description: "Próxima ação e condições", icon: LayoutDashboard },
  { href: "#indicadores", label: "Indicadores", description: "Funil e ritmo de envio", icon: ChartNoAxesColumnIncreasing },
  { href: "#agir-agora", label: "Agir agora", description: "Contas com ação disponível", icon: Building2 },
  { href: "#precisa-contato", label: "Precisa de contato", description: "Destinatários a resolver", icon: ContactRound },
  { href: "#pendencias", label: "Pendências", description: "Decisões humanas", icon: CircleAlert },
  { href: "#planejamento", label: "Planejamento", description: "Contas prontas", icon: ListChecks },
  { href: "#revisao", label: "Revisão", description: "Mensagens antes do envio", icon: ListChecks },
];

export default function ConfengeOperatorLayout() {
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<AppError | null>(null);

  useEffect(() => {
    document.documentElement.lang = "pt-BR";
    document.title = "Central comercial | CONFENGE";
    void ensureConfengeOperatorSession(true)
      .then(() => setReady(true))
      .catch((reason: AppError) => setError(reason));
  }, []);

  if (error) {
    return (
      <OperatorState
        title="Não foi possível abrir a Central comercial"
        message="A sessão do operador não está disponível. Verifique a configuração do servidor e tente novamente."
        requestId={error.request_id}
      />
    );
  }

  if (!ready) {
    return <OperatorState title="Abrindo a Central comercial" message="Preparando seu ambiente de trabalho…" loading />;
  }

	return (
		<ConfirmProvider>
			<SocketProvider>
				<OperatorShell />
			</SocketProvider>
		</ConfirmProvider>
	);
}

function OperatorShell() {
  const [navOpen, setNavOpen] = useState(false);
  const { isConnected } = useSocket();
  const queryClient = useQueryClient();
  const status = useConfengeStatus();
  const enabled = !!status.data?.enabled;
  const summary = useConfengeSummary(enabled);
  const overview = useConfengeWorkingOverview(enabled);
  const review = useConfengeReviewTouchpoints(enabled);
  const pilotGate = buildPilotGate({
    readiness: status.data?.readiness,
    summary: summary.data,
    overview: overview.data,
    reviewCount: review.data?.length,
  });
  const criticalCount = pilotGate.blockers.length;
  const identity = useQuery({
    queryKey: ["confenge-operator", "identity"],
    queryFn: () => Request<OperatorIdentity>({ method: "GET", url: "/me", authorization: true }),
    staleTime: 5 * 60_000,
  });
  const userId = identity.data?.user_id;
  const orgId = identity.data?.organization_id;
  const { joinChannel, leaveChannel, subscribeToChannel } = useSocket();

  useEffect(() => {
    if (!isConnected) return;
    const topics = [userId ? `user:${userId}` : null, orgId ? `org:${orgId}` : null].filter(Boolean) as string[];
    const refresh = (payload: Record<string, unknown>) => {
      const event = String(payload.event_type ?? payload.type ?? "").toUpperCase();
      if (event.startsWith("PRESENCE") || event === "RATE_LIMITED") return;
      void queryClient.invalidateQueries({ queryKey: ["confenge"] });
    };
    const unsubscribers = topics.map((topic) => {
      joinChannel(topic);
      return subscribeToChannel(topic, "*", refresh);
    });
    void queryClient.invalidateQueries({ queryKey: ["confenge"] });
    return () => {
      unsubscribers.forEach((unsubscribe) => unsubscribe());
      topics.forEach(leaveChannel);
    };
  }, [isConnected, userId, orgId, joinChannel, leaveChannel, subscribeToChannel, queryClient]);

  return (
    <div className="fixed inset-0 flex bg-slate-50 text-slate-900">
      <OperatorSidebar
        mobile={false}
        organizationName={identity.data?.organization_name}
        isConnected={isConnected}
        criticalCount={criticalCount}
      />

      {navOpen && (
        <div className="fixed inset-0 z-40 md:hidden" aria-label="Navegação da Central comercial">
          <button
            type="button"
            className="absolute inset-0 bg-slate-950/30"
            aria-label="Fechar menu"
            onClick={() => setNavOpen(false)}
          />
          <OperatorSidebar
            mobile
            organizationName={identity.data?.organization_name}
            isConnected={isConnected}
            criticalCount={criticalCount}
            onNavigate={() => setNavOpen(false)}
            onClose={() => setNavOpen(false)}
          />
        </div>
      )}

      <div className="min-w-0 flex-1 flex flex-col">
        <header className="h-12 shrink-0 px-3 md:px-5 flex items-center border-b border-slate-200 bg-white">
          <button
            type="button"
            className="md:hidden h-8 w-8 mr-2 inline-flex items-center justify-center rounded-md border border-slate-200 text-slate-600"
            aria-label="Abrir menu"
            onClick={() => setNavOpen(true)}
          >
            <Menu className="h-4 w-4" />
          </button>
          <div>
            <div className="font-semibold text-[13px]">Central comercial</div>
            <div className="text-[10.5px] text-slate-500">Operação de prospecção CONFENGE</div>
          </div>
          <div className="ml-auto flex items-center gap-2 text-[11.5px] text-slate-500">
            <span className="hidden sm:inline">{identity.data?.organization_name || "Operação comercial"}</span>
            {isConnected ? <Wifi className="h-3.5 w-3.5 text-emerald-600" aria-label="Atualização em tempo real conectada" /> : <WifiOff className="h-3.5 w-3.5 text-amber-600" aria-label="Reconectando atualização em tempo real" />}
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto"><Outlet /></main>
      </div>
    </div>
  );
}

function OperatorSidebar({
  mobile,
  organizationName,
  isConnected,
  criticalCount,
  onNavigate,
  onClose,
}: {
  mobile: boolean;
  organizationName?: string;
  isConnected: boolean;
  criticalCount: number;
  onNavigate?: () => void;
  onClose?: () => void;
}) {
  return (
    <aside
      className={
        mobile
          ? "absolute inset-y-0 left-0 z-10 flex w-[252px] flex-col border-r border-slate-200 bg-white shadow-xl"
          : "hidden md:flex w-[232px] shrink-0 flex-col border-r border-slate-200 bg-white"
      }
    >
      <div className="h-[76px] shrink-0 px-4 flex items-center border-b border-slate-100">
        <img
          src="/confenge/logo-confenge.png"
          alt="CONFENGE Inteligência Técnica"
          className="h-auto w-[166px] max-w-full object-contain object-left"
        />
        {mobile && (
          <button
            type="button"
            className="ml-auto h-8 w-8 inline-flex items-center justify-center rounded-md text-slate-500 hover:bg-slate-100"
            aria-label="Fechar menu"
            onClick={onClose}
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      <nav className="flex-1 overflow-auto px-2 py-3" aria-label="Áreas da Central comercial">
        <div className="px-2 pb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400">Operação comercial</div>
        <ul className="space-y-0.5">
          {OPERATOR_NAV.map((item) => (
            <li key={item.href}>
              <a
                href={item.href}
                onClick={onNavigate}
                className={
                  item.href === "#pendencias-criticas" && criticalCount > 0
                    ? "group flex min-h-11 items-start gap-2.5 rounded-md border border-rose-200 bg-rose-50 px-2 py-2 text-rose-800 hover:bg-rose-100"
                    : "group flex min-h-11 items-start gap-2.5 rounded-md px-2 py-2 text-slate-600 hover:bg-sky-50 hover:text-sky-800"
                }
              >
                <item.icon
                  className={`mt-0.5 h-4 w-4 shrink-0 ${item.href === "#pendencias-criticas" && criticalCount > 0 ? "text-rose-600" : "text-slate-400 group-hover:text-sky-600"}`}
                  strokeWidth={1.7}
                />
                <span className="min-w-0">
                  <span className="block text-[12.5px] font-medium leading-4">{item.label}</span>
                  <span className="block truncate text-[10.5px] leading-4 text-slate-400">{item.description}</span>
                </span>
                {item.href === "#pendencias-criticas" && criticalCount > 0 && (
                  <span className="ml-auto min-w-5 rounded-full bg-rose-600 px-1.5 py-0.5 text-center text-[10px] font-semibold text-white">
                    {criticalCount}
                  </span>
                )}
              </a>
            </li>
          ))}
        </ul>
      </nav>

      <div className="border-t border-slate-100 px-4 py-3">
        <div className="truncate text-[11.5px] font-medium text-slate-700">{organizationName || "Operação comercial"}</div>
        <div className="mt-1 flex items-center gap-1.5 text-[10.5px] text-slate-500">
          <span className={`h-1.5 w-1.5 rounded-full ${isConnected ? "bg-emerald-500" : "bg-amber-500"}`} />
          {isConnected ? "Atualização em tempo real" : "Reconectando atualizações"}
        </div>
        <div className="mt-2 text-[9.5px] text-slate-400">Operado com tecnologia Warmbly</div>
      </div>
    </aside>
  );
}

function OperatorState({ title, message, requestId, loading = false }: { title: string; message: string; requestId?: string; loading?: boolean }) {
  return (
    <main className="min-h-screen bg-slate-50 flex items-center justify-center p-6">
      <div className="w-full max-w-md rounded-md border border-slate-200 bg-white p-6 text-center">
        {loading && <Loader2 className="h-5 w-5 animate-spin text-sky-600 mx-auto mb-3" />}
        <h1 className="text-base font-semibold text-slate-900">{title}</h1>
        <p className="mt-2 text-[12.5px] text-slate-600">{message}</p>
        {requestId && <p className="mt-3 text-[11px] text-slate-400">Código de atendimento: {requestId}</p>}
      </div>
    </main>
  );
}
