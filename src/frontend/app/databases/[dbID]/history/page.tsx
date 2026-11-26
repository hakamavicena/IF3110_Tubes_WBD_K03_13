import { Suspense } from "react";
import { MigrationHistory } from "@/components/migration-history";
import { LoadingPanel } from "@/components/workspace-shell";

export default function MigrationHistoryPage({
  params,
}: {
  params: Promise<{ dbID: string }>;
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
  params: Promise<{ dbID: string }>;
}) {
  const { dbID } = await params;
  return <MigrationHistory databaseId={dbID} />;
}
