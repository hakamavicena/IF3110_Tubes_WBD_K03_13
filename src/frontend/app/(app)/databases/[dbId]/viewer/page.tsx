import { Suspense } from "react";
import { DatabaseExplorer } from "@/components/database-explorer";
import { LoadingPanel } from "@/components/workspace-shell";

export default function DatabaseViewerPage({
  params,
}: PageProps<"/databases/[dbId]/viewer">) {
  return (
    <Suspense fallback={<LoadingPanel label="Opening schema explorer…" />}>
      <DatabaseViewerContent params={params} />
    </Suspense>
  );
}

async function DatabaseViewerContent({
  params,
}: {
  params: Promise<{ dbId: string }>;
}) {
  const { dbId } = await params;
  return <DatabaseExplorer databaseId={dbId} />;
}
