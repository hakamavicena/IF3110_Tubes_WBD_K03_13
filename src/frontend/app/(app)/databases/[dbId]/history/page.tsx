import { Suspense } from "react";
import { MigrationHistory } from "@/components/migration-history";
import { LoadingPanel } from "@/components/workspace-shell";

export default function MigrationHistoryPage({
  params,
}: {
  params: Promise<{ dbId: string }>;
}) {
  return (
    <Suspense fallback={<LoadingPanel label="Opening migration history…" />}>
      <MigrationHistoryContent params={params} />
    </Suspense>
  );
}

async function MigrationHistoryContent({
  params,
}: {
  params: Promise<{ dbId: string }>;
}) {
  const { dbId } = await params;
  return <MigrationHistory databaseId={dbId} />;
}
