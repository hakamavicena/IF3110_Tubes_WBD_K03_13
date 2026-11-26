"use client";

import { useCallback, useEffect, useState } from "react";
import { errorMessage } from "@/lib/api";

// Memuat data async dengan state loading/error dan fungsi reload.
export function useLoad<T>(loader: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    try {
      const result = await loader();
      setData(result);
      setError("");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch saat mount; state baru diset setelah await
    void reload();
  }, [reload]);

  return { data, error, loading, reload };
}
