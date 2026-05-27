import { BackendsPage } from "./BackendsPage";

export default function MessagingPage() {
    return (
        <BackendsPage
            kind="eventbus"
            title="Messaging (EventBus)"
            description="Kafka-style event bus shared by backend → consumer → worker. Workers receive commands on worker-specific topics and publish results back through the same bus."
            notes="Workers should not depend on direct PostgreSQL access; Kafka is the line of communication into and out of the execution plane. Provider swaps here affect the routing layer, not on-the-wire topic naming."
        />
    );
}
