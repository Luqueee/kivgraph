import { useEffect, useMemo, useRef, useState } from "react";

import {
  ApiError,
  fetchMeta,
  fetchNeighborhood,
  fetchSymbol,
  searchSymbols,
  type NeighborhoodResponse,
  type SearchMode,
  type SnapshotMeta,
  type SymbolView,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

const SEARCH_DEBOUNCE_MS = 250;
const MAX_NEIGHBORHOOD_DEPTH = 3;
const ALL_FILTER = "__all__";

export interface ViewerChromeProps {
  readonly meta: SnapshotMeta | null;
  readonly loading: boolean;
  readonly error: string | null;
  readonly snapshotChanged: boolean;
  readonly onReload: () => void;
  readonly onSnapshotChanged: () => void;
}

function describe(error: unknown): string {
  if (error instanceof ApiError) return `${error.code}: ${error.message}`;
  if (error instanceof Error) return error.message;
  return "unknown error";
}

function isCurrentSnapshot(
  snapshotId: number,
  meta: SnapshotMeta | null,
): boolean {
  return (
    meta === null || meta.snapshotId === null || meta.snapshotId === snapshotId
  );
}

export function filterSymbols(
  symbols: readonly SymbolView[],
  repository: string,
  kind: string,
): readonly SymbolView[] {
  return symbols.filter(
    (symbol) =>
      (repository === ALL_FILTER || symbol.repository === repository) &&
      (kind === ALL_FILTER || symbol.kind === kind),
  );
}

export function filterNeighborhoodEdges(
  neighborhood: NeighborhoodResponse | null,
  confidence: string,
): readonly NeighborhoodResponse["edges"][number][] {
  if (neighborhood === null) return [];
  if (confidence === ALL_FILTER) return neighborhood.edges;
  return neighborhood.edges.filter((edge) => edge.confidence === confidence);
}

interface FilterSelectProps {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly options: readonly string[];
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: FilterSelectProps): React.ReactElement {
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-1 text-[10px] uppercase tracking-wide text-muted-foreground">
      <span>{label}</span>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger className="h-7 w-full text-xs normal-case tracking-normal">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_FILTER}>all</SelectItem>
          {options.map((option) => (
            <SelectItem key={option} value={option}>
              {option}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function SymbolResult({
  symbol,
  selected,
  onSelect,
}: {
  readonly symbol: SymbolView;
  readonly selected: boolean;
  readonly onSelect: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      className={cn(
        "flex w-full flex-col gap-0.5 rounded-lg border px-2.5 py-2 text-left transition-colors",
        selected
          ? "border-primary/60 bg-primary/15 text-foreground"
          : "border-border/60 bg-background/40 text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
      onClick={onSelect}
      aria-label={`Open ${symbol.qualifiedName || symbol.name}`}
    >
      <span className="truncate text-xs font-medium text-foreground">
        {symbol.qualifiedName || symbol.name}
      </span>
      <span className="truncate text-[10px]">
        {symbol.repository} · {symbol.file}:{symbol.startLine}
      </span>
      <span className="flex items-center gap-1.5 text-[10px]">
        <Badge variant="outline" className="h-4 px-1.5 text-[9px]">
          {symbol.kind}
        </Badge>
        <span className="truncate">{symbol.package}</span>
      </span>
    </button>
  );
}

function SymbolDetails({
  symbol,
}: {
  readonly symbol: SymbolView;
}): React.ReactElement {
  return (
    <div className="grid gap-1.5 text-[11px]">
      <div className="flex items-start justify-between gap-3">
        <span className="text-muted-foreground">kind</span>
        <Badge variant="secondary">{symbol.kind}</Badge>
      </div>
      <div className="flex items-start justify-between gap-3">
        <span className="text-muted-foreground">repository</span>
        <span className="max-w-[15rem] text-right text-foreground">
          {symbol.repository}
        </span>
      </div>
      <div className="flex items-start justify-between gap-3">
        <span className="text-muted-foreground">package</span>
        <span className="max-w-[15rem] truncate text-right text-foreground">
          {symbol.package}
        </span>
      </div>
      <div className="flex items-start justify-between gap-3">
        <span className="text-muted-foreground">file</span>
        <span className="max-w-[15rem] truncate text-right text-foreground">
          {symbol.file}:{symbol.startLine}-{symbol.endLine}
        </span>
      </div>
      <div className="border-t border-border/60 pt-1.5">
        <span className="text-muted-foreground">signature</span>
        <code className="mt-1 block max-h-16 overflow-auto whitespace-pre-wrap break-words text-[10px] text-foreground">
          {symbol.signature || symbol.qualifiedName || symbol.name}
        </code>
      </div>
      <div className="border-t border-border/60 pt-1.5">
        <span className="text-muted-foreground">stable key</span>
        <code className="mt-1 block truncate text-[10px] text-foreground">
          {symbol.stableKey}
        </code>
      </div>
    </div>
  );
}

function SnapshotMessage({
  changed,
  onReload,
}: {
  readonly changed: boolean;
  readonly onReload: () => void;
}): React.ReactElement | null {
  if (!changed) return null;
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg border border-amber-400/40 bg-amber-500/10 px-2.5 py-2 text-xs text-amber-100">
      <span>published snapshot changed</span>
      <Button type="button" size="xs" variant="outline" onClick={onReload}>
        reload
      </Button>
    </div>
  );
}

export function ViewerChrome({
  meta,
  loading,
  error,
  snapshotChanged,
  onReload,
  onSnapshotChanged,
}: ViewerChromeProps): React.ReactElement {
  const [query, setQuery] = useState("");
  const [mode, setMode] = useState<SearchMode>("prefix");
  const [results, setResults] = useState<readonly SymbolView[]>([]);
  const [resultTotal, setResultTotal] = useState(0);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [selected, setSelected] = useState<SymbolView | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [neighborhood, setNeighborhood] = useState<NeighborhoodResponse | null>(
    null,
  );
  const [neighborhoodLoading, setNeighborhoodLoading] = useState(false);
  const [depth, setDepth] = useState(1);
  const [repository, setRepository] = useState(ALL_FILTER);
  const [kind, setKind] = useState(ALL_FILTER);
  const [confidence, setConfidence] = useState(ALL_FILTER);
  const searchGeneration = useRef(0);
  const symbolAbort = useRef<AbortController | null>(null);
  const neighborhoodAbort = useRef<AbortController | null>(null);

  useEffect(
    () => () => {
      symbolAbort.current?.abort();
      neighborhoodAbort.current?.abort();
    },
    [],
  );

  useEffect(() => {
    if (meta?.snapshotId === null || meta === null) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      void fetchMeta(controller.signal)
        .then((next) => {
          if (next.snapshotId !== meta.snapshotId) onSnapshotChanged();
        })
        .catch(() => {
          // The active graph remains usable when a background health check fails.
        });
    }, 15_000);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [meta, onSnapshotChanged]);

  useEffect(() => {
    searchGeneration.current += 1;
    const generation = searchGeneration.current;
    const trimmed = query.trim();
    if (trimmed.length < 2) {
      setResults([]);
      setResultTotal(0);
      setSearchError(null);
      setSearching(false);
      return;
    }

    const controller = new AbortController();
    setSearching(true);
    setSearchError(null);
    const timer = window.setTimeout(() => {
      void searchSymbols(
        trimmed,
        mode,
        meta?.snapshotId ?? null,
        controller.signal,
      )
        .then((response) => {
          if (generation !== searchGeneration.current) return;
          if (!isCurrentSnapshot(response.snapshotId, meta)) {
            onSnapshotChanged();
            return;
          }
          setResults(response.results);
          setResultTotal(response.total);
          setSearching(false);
        })
        .catch((requestError: unknown) => {
          if (
            controller.signal.aborted ||
            generation !== searchGeneration.current
          )
            return;
          setSearching(false);
          setSearchError(describe(requestError));
        });
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [meta, mode, onSnapshotChanged, query]);

  useEffect(() => {
    if (selected === null) {
      setNeighborhood(null);
      setNeighborhoodLoading(false);
      return;
    }
    neighborhoodAbort.current?.abort();
    const controller = new AbortController();
    neighborhoodAbort.current = controller;
    setNeighborhoodLoading(true);
    setDetailError(null);
    void fetchNeighborhood(
      selected.stableKey,
      depth,
      "both",
      meta?.snapshotId ?? null,
      controller.signal,
    )
      .then((response) => {
        if (!isCurrentSnapshot(response.snapshotId, meta)) {
          onSnapshotChanged();
          return;
        }
        setNeighborhood(response);
        setNeighborhoodLoading(false);
      })
      .catch((requestError: unknown) => {
        if (controller.signal.aborted) return;
        setNeighborhoodLoading(false);
        setDetailError(describe(requestError));
      });
    return () => controller.abort();
  }, [depth, meta, onSnapshotChanged, selected]);

  const repositories = useMemo(
    () => [...new Set(results.map((symbol) => symbol.repository))].sort(),
    [results],
  );
  const kinds = useMemo(
    () => [...new Set(results.map((symbol) => symbol.kind))].sort(),
    [results],
  );
  const filteredResults = useMemo(
    () => filterSymbols(results, repository, kind),
    [kind, repository, results],
  );
  const visibleEdges = useMemo(
    () => filterNeighborhoodEdges(neighborhood, confidence),
    [confidence, neighborhood],
  );
  const emptySnapshot =
    !loading &&
    error === null &&
    meta !== null &&
    meta.counts.repositories === 0 &&
    meta.counts.packages === 0 &&
    meta.counts.files === 0 &&
    meta.counts.symbols === 0;

  const openSymbol = (symbol: SymbolView): void => {
    symbolAbort.current?.abort();
    const controller = new AbortController();
    symbolAbort.current = controller;
    setSelected(symbol);
    setDepth(1);
    setNeighborhood(null);
    setDetailError(null);
    void fetchSymbol(
      symbol.stableKey,
      meta?.snapshotId ?? null,
      controller.signal,
    )
      .then((response) => {
        if (!isCurrentSnapshot(response.snapshotId, meta)) {
          onSnapshotChanged();
          return;
        }
        setSelected(response.symbol);
      })
      .catch((requestError: unknown) => {
        if (controller.signal.aborted) return;
        setDetailError(describe(requestError));
      });
  };

  return (
    <aside
      className="pointer-events-auto absolute left-4 top-16 z-20 flex max-h-[calc(100svh-8rem)] w-[min(24rem,calc(100vw-2rem))] flex-col gap-3 overflow-y-auto rounded-2xl border border-border/80 bg-background/90 p-3 text-foreground shadow-2xl backdrop-blur"
      data-testid="viewer-chrome"
      aria-label="Graph explorer"
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <h1 className="text-sm font-semibold">Explore snapshot</h1>
          <p className="text-[10px] text-muted-foreground">
            read-only · snapshot {meta?.snapshotId ?? "not ready"}
          </p>
        </div>
        {meta?.status ? <Badge variant="outline">{meta.status}</Badge> : null}
      </div>

      <SnapshotMessage changed={snapshotChanged} onReload={onReload} />

      {loading ? (
        <div className="rounded-lg border border-border/60 bg-muted/20 px-2.5 py-2 text-xs text-muted-foreground">
          loading snapshot…
        </div>
      ) : null}
      {error ? (
        <div
          className="grid gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-2.5 py-2 text-xs text-destructive-foreground"
          role="alert"
        >
          <span>{error}</span>
          <Button type="button" size="xs" variant="outline" onClick={onReload}>
            retry
          </Button>
        </div>
      ) : null}
      {emptySnapshot ? (
        <div className="rounded-lg border border-border/60 bg-muted/20 px-2.5 py-2 text-xs text-muted-foreground">
          published snapshot is empty
        </div>
      ) : null}

      <div className="grid gap-2 border-t border-border/60 pt-3">
        <div className="flex gap-2">
          <Input
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="Search symbol or qualified name"
            aria-label="Search symbols"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setQuery("")}
            disabled={query.length === 0}
          >
            clear
          </Button>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
            mode
          </span>
          <Select
            value={mode}
            onValueChange={(value) => setMode(value as SearchMode)}
          >
            <SelectTrigger className="h-7 flex-1 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="prefix">prefix</SelectItem>
              <SelectItem value="exact">exact</SelectItem>
              <SelectItem value="qualified_exact">qualified exact</SelectItem>
            </SelectContent>
          </Select>
          {searching ? (
            <span className="text-[10px] text-muted-foreground">
              searching…
            </span>
          ) : null}
        </div>
      </div>

      {searchError ? (
        <div
          className="rounded-lg border border-destructive/40 bg-destructive/10 px-2.5 py-2 text-xs text-destructive-foreground"
          role="alert"
        >
          {searchError}
        </div>
      ) : null}

      {results.length > 0 ? (
        <div className="grid gap-2 border-t border-border/60 pt-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
              results · {filteredResults.length}/{resultTotal}
            </span>
            {resultTotal > filteredResults.length ? (
              <Badge variant="outline">filtered</Badge>
            ) : null}
          </div>
          <div className="flex gap-2">
            <FilterSelect
              label="repository"
              value={repository}
              onChange={setRepository}
              options={repositories}
            />
            <FilterSelect
              label="kind"
              value={kind}
              onChange={setKind}
              options={kinds}
            />
          </div>
          <div className="grid max-h-56 gap-1.5 overflow-y-auto pr-1">
            {filteredResults.map((symbol) => (
              <SymbolResult
                key={symbol.stableKey}
                symbol={symbol}
                selected={selected?.stableKey === symbol.stableKey}
                onSelect={() => openSymbol(symbol)}
              />
            ))}
          </div>
        </div>
      ) : query.trim().length >= 2 && !searching && !searchError ? (
        <p className="border-t border-border/60 pt-3 text-xs text-muted-foreground">
          no symbols found
        </p>
      ) : null}

      {selected ? (
        <div className="grid gap-2 border-t border-border/60 pt-3">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <h2 className="truncate text-xs font-semibold">
                {selected.qualifiedName || selected.name}
              </h2>
              <p className="truncate text-[10px] text-muted-foreground">
                {selected.canonicalIdentity}
              </p>
            </div>
            <Badge>{selected.language || "unknown"}</Badge>
          </div>
          <SymbolDetails symbol={selected} />
          {detailError ? (
            <p className="text-xs text-destructive-foreground" role="alert">
              {detailError}
            </p>
          ) : null}
          <div className="flex items-center justify-between gap-2 border-t border-border/60 pt-2">
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
              neighborhood
            </span>
            <div className="flex items-center gap-1.5">
              <Badge variant="outline">depth {depth}</Badge>
              {depth < MAX_NEIGHBORHOOD_DEPTH ? (
                <Button
                  type="button"
                  size="xs"
                  variant="outline"
                  onClick={() => setDepth((value) => value + 1)}
                >
                  expand
                </Button>
              ) : null}
            </div>
          </div>
          <FilterSelect
            label="edge confidence"
            value={confidence}
            onChange={setConfidence}
            options={[
              "EXACT_TYPECHECKED",
              "EXACT_DECLARATION_MAPPED",
              "EXACT_PACKAGE_MAPPED",
              "STRUCTURAL_CERTAIN",
              "CANDIDATE",
              "UNRESOLVED",
            ]}
          />
          {neighborhoodLoading ? (
            <p className="text-xs text-muted-foreground">
              loading neighborhood…
            </p>
          ) : neighborhood ? (
            <div className="grid gap-1.5 text-[10px] text-muted-foreground">
              <p>
                {neighborhood.nodes.length} nodes · {visibleEdges.length} edges
                {neighborhood.truncated ? " · truncated" : ""}
              </p>
              <div className="max-h-32 overflow-y-auto rounded-lg border border-border/60 bg-muted/10 p-2">
                {visibleEdges.length === 0 ? (
                  <p>no edges match this confidence</p>
                ) : (
                  visibleEdges.slice(0, 20).map((edge) => (
                    <p
                      key={`${edge.source}:${edge.target}:${edge.kind}`}
                      className="truncate"
                    >
                      {edge.kind} · {edge.confidence} · {edge.source} →{" "}
                      {edge.target}
                    </p>
                  ))
                )}
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </aside>
  );
}
