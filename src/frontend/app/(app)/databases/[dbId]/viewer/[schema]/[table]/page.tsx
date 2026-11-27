import { Suspense } from "react";
import { TableInspector } from "@/components/table-inspector";
import { LoadingPanel } from "@/components/workspace-shell";

export default function TableViewerPage({
  params,
}: {
  params: Promise<{ dbId: string; schema: string; table: string }>;
}) {
  return (
    <Suspense fallback={<LoadingPanel label="Opening table details…" />}>
      <TableViewerContent params={params} />
    </Suspense>
  );
}

async function TableViewerContent({
  params,
}: {
  params: Promise<{ dbId: string; schema: string; table: string }>;
}) {
  const { dbId, schema, table } = await params;
  return (
    <TableInspector
      databaseId={dbId}
      schemaName={schema}
      tableName={table}
    />
  );
}
