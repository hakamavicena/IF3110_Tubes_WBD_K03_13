import { Suspense } from "react";
import { TableInspector } from "@/components/table-inspector";
import { LoadingPanel } from "@/components/workspace-shell";

export default function TableViewerPage({
  params,
}: {
  params: Promise<{ dbID: string; schema: string; table: string }>;
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
  params: Promise<{ dbID: string; schema: string; table: string }>;
}) {
  const { dbID, schema, table } = await params;
  return (
    <TableInspector
      databaseId={dbID}
      schemaName={schema}
      tableName={table}
    />
  );
}
