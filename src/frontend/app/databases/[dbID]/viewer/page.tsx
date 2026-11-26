import { Suspense } from "react";
import { DatabaseExplorer } from "@/components/database-explorer";
import { LoadingPanel } from "@/components/workspace-shell";

export default function DatabaseViewerPage({
  params,
}: PageProps<"/databases/[dbID]/viewer">) {
  return (
    <Suspense fallback={<LoadingPanel label="Opening schema explorer…" />}>
      <DatabaseViewerContent params={params} />
    </Suspense>
  );
}

async function DatabaseViewerContent({
  params,
}: {
  params: Promise<{ dbID: string }>;
}) {
  const { dbID } = await params;
  return <DatabaseExplorer databaseId={dbID} />;
}
