import { expect, test } from "@playwright/test";

const REPOSITORY_COUNT = 53;
const RELATIONSHIP_COUNT = 10_000;
const NODE_COUNT = REPOSITORY_COUNT * 2 + 1;
const DISTINCT_PAIR_COUNT = REPOSITORY_COUNT - 1;
const DEEP_NODE_COUNT = 5_000;
const ACCESSIBLE_RELATIONSHIP_COUNT = 501;

function repositoryID(index: number): string {
  return `repo-${String(index).padStart(2, "0")}`;
}

function deepRepositoryID(index: number): string {
  return `deep-${String(index).padStart(4, "0")}`;
}

function largeTopologyPayload(generation = "000107"): object {
  const repositories = Array.from({ length: REPOSITORY_COUNT }, (_, index) => {
    const id = repositoryID(index);
    return { id, name: id, languages: ["typescript"] };
  });
  const worktrees = repositories.map((repository) => ({
    id: `worktree-${repository.id}`,
    repository: repository.id,
    path: `/workspace/${repository.id}`,
  }));
  const relationships = Array.from(
    { length: RELATIONSHIP_COUNT },
    (_, index) => {
      const sourceIndex = index % (REPOSITORY_COUNT - 1);
      return {
        profile: "default",
        type: "code_dependency",
        source: { type: "repository", id: repositoryID(sourceIndex) },
        target: { type: "repository", id: repositoryID(sourceIndex + 1) },
        kind: "CALLS_DIRECT",
        status: "exact",
        confidence: "EXACT_TYPECHECKED",
        provenance: "TYPESCRIPT_CHECKER",
        evidence: `fixture.ts:${index + 1}`,
      };
    },
  );

  return {
    api_version: "v1",
    topology_version: 1,
    status: "ready",
    generation_id: generation,
    selected_profiles: ["default"],
    profiles: [
      {
        id: "default",
        generation_id: generation,
        status: "ready",
        composition_complete: true,
        worktrees: worktrees.map((worktree) => worktree.id),
      },
    ],
    repositories,
    worktrees,
    sources: [],
    shared_inputs: [],
    relationships,
    completeness: { complete: true, truncated: false },
  };
}

function dependencyTopologyPayload(count: number, cycle = false): object {
  const repositories = Array.from({ length: count }, (_, index) => {
    const id = deepRepositoryID(index);
    return { id, name: id, languages: ["typescript"] };
  });
  const relationshipCount = cycle ? count : count - 1;
  const relationships = Array.from(
    { length: relationshipCount },
    (_, index) => ({
      profile: "default",
      type: "code_dependency",
      source: { type: "repository", id: deepRepositoryID(index) },
      target: {
        type: "repository",
        id: deepRepositoryID(cycle ? (index + 1) % count : index + 1),
      },
      kind: "CALLS_DIRECT",
      status: "exact",
      confidence: "EXACT_TYPECHECKED",
      provenance: "TYPESCRIPT_CHECKER",
      evidence: `deep.ts:${index + 1}`,
    }),
  );

  return {
    api_version: "v1",
    topology_version: 1,
    status: "ready",
    selected_profiles: [],
    profiles: [],
    repositories,
    worktrees: [],
    sources: [],
    shared_inputs: [],
    relationships,
    completeness: { complete: true, truncated: false },
  };
}

function groupedSemanticsTopologyPayload(): object {
  const variants = [
    {
      status: "exact",
      confidence: "EXACT_TYPECHECKED",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "TYPESCRIPT_CHECKER",
    },
    {
      status: "candidate",
      confidence: "CANDIDATE",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "TEXTUAL_CANDIDATE",
    },
    {
      status: "unresolved",
      confidence: "UNRESOLVED",
      type: "unresolved_reference",
      kind: "UNRESOLVED_REFERENCE",
      provenance: "UNRESOLVED_REFERENCE",
    },
    {
      status: "conflict",
      confidence: "CONFLICT",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "CONFLICTING_EVIDENCE",
    },
    {
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      type: "membership",
      kind: "contains",
      provenance: "TOPOLOGY_DECLARATION",
    },
  ];
  const relationships = variants.flatMap((variant) =>
    [0, 1].map((occurrence) => ({
      ...variant,
      source: { type: "repository", id: "source" },
      target: { type: "repository", id: "target" },
      evidence: `source.ts:${occurrence + 1}`,
    })),
  );

  return {
    api_version: "v1",
    topology_version: 1,
    status: "ready",
    selected_profiles: [],
    profiles: [],
    repositories: [
      { id: "source", name: "source", languages: ["typescript"] },
      { id: "target", name: "target", languages: ["typescript"] },
    ],
    worktrees: [],
    sources: [],
    shared_inputs: [],
    relationships,
    completeness: { complete: true, truncated: false },
  };
}

function accessibleTopologyPayload(): object {
  const relationships = Array.from(
    { length: ACCESSIBLE_RELATIONSHIP_COUNT },
    (_, index) => ({
      profile: "default",
      type: "code_dependency",
      source: { type: "repository", id: "source" },
      target: { type: "repository", id: "target" },
      kind: "CALLS_DIRECT",
      status: "exact",
      confidence: "EXACT_TYPECHECKED",
      provenance: "TYPESCRIPT_CHECKER",
      evidence: `accessible.ts:${index + 1}`,
    }),
  );

  return {
    api_version: "v1",
    topology_version: 1,
    status: "ready",
    generation_id: "000199",
    selected_profiles: ["default"],
    profiles: [
      {
        id: "default",
        generation_id: "000199",
        status: "ready",
        composition_complete: true,
        worktrees: ["source-worktree", "target-worktree"],
      },
    ],
    repositories: [
      { id: "source", name: "source", languages: ["typescript"] },
      { id: "target", name: "target", languages: ["typescript"] },
    ],
    worktrees: [
      {
        id: "source-worktree",
        repository: "source",
        path: "/workspace/source",
      },
      {
        id: "target-worktree",
        repository: "target",
        path: "/workspace/target",
      },
    ],
    sources: [
      {
        profile: "default",
        repository: "source",
        worktree: "source-worktree",
        status: "stale",
        reason: "working tree changed after indexing",
        indexed: {
          id: "indexed-source",
          worktree: "source-worktree",
          branch: "main",
          commit: "indexed-commit",
          dirty: false,
          content_digest: "indexed-digest",
        },
        current: {
          id: "current-source",
          worktree: "source-worktree",
          branch: "feature/accessibility",
          commit: "current-commit",
          dirty: true,
          content_digest: "current-digest",
        },
      },
    ],
    shared_inputs: [],
    relationships,
    completeness: {
      complete: false,
      truncated: true,
      reason: "server result limit",
    },
  };
}

function overlayInvalidationTopologyPayload(): object {
  return {
    api_version: "v1",
    topology_version: 2,
    status: "stale",
    selected_profiles: ["default", "feature", "maintenance"],
    profiles: [
      {
        id: "default",
        generation_id: "000007",
        status: "stale",
        composition_complete: true,
        worktrees: ["shared-main"],
      },
      {
        id: "feature",
        generation_id: "000009",
        status: "ready",
        composition_complete: true,
        worktrees: ["feature-worktree"],
      },
      {
        id: "maintenance",
        generation_id: "000008",
        status: "stale",
        composition_complete: true,
        worktrees: ["shared-main"],
      },
    ],
    repositories: [{ id: "repo", name: "Repository", languages: ["go"] }],
    worktrees: [
      {
        id: "shared-main",
        repository: "repo",
        path: "/workspace/shared",
      },
      {
        id: "feature-worktree",
        repository: "repo",
        path: "/workspace/feature",
      },
    ],
    sources: [
      {
        profile: "default",
        repository: "repo",
        worktree: "shared-main",
        status: "stale",
        reason: "shared content changed after indexing",
      },
      {
        profile: "maintenance",
        repository: "repo",
        worktree: "shared-main",
        status: "stale",
        reason: "shared content changed after indexing",
      },
    ],
    shared_inputs: [
      {
        type: "worktree",
        id: "shared-main",
        repository: "repo",
        owners: ["default", "maintenance"],
        status: "stale",
        reason: "shared content changed after indexing",
      },
    ],
    relationships: [
      {
        profile: "default",
        generation_id: "000007",
        type: "shared_input_usage",
        source: { type: "profile", id: "default" },
        target: { type: "shared_input", id: "worktree:shared-main" },
        kind: "uses",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "TOPOLOGY_DECLARATION",
      },
      {
        profile: "maintenance",
        generation_id: "000008",
        type: "shared_input_usage",
        source: { type: "profile", id: "maintenance" },
        target: { type: "shared_input", id: "worktree:shared-main" },
        kind: "uses",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "TOPOLOGY_DECLARATION",
      },
      {
        profile: "feature",
        generation_id: "000009",
        type: "worktree_overlay",
        source: { type: "worktree", id: "feature-worktree" },
        target: { type: "shared_input", id: "worktree:shared-main" },
        kind: "overlays",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "TOPOLOGY_DECLARATION",
      },
      {
        profile: "default",
        generation_id: "000007",
        type: "shared_input_invalidation",
        source: { type: "shared_input", id: "worktree:shared-main" },
        target: { type: "profile", id: "default" },
        kind: "invalidates",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "SOURCE_INVALIDATION",
        reason: "shared content changed after indexing",
      },
      {
        profile: "maintenance",
        generation_id: "000008",
        type: "shared_input_invalidation",
        source: { type: "shared_input", id: "worktree:shared-main" },
        target: { type: "profile", id: "maintenance" },
        kind: "invalidates",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "SOURCE_INVALIDATION",
        reason: "shared content changed after indexing",
      },
    ],
    completeness: { complete: true, truncated: false },
  };
}

test("keeps a large topology explorable", async ({ page }) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(largeTopologyPayload()),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(page.getByTestId("topology-explorer")).toBeVisible();
  await expect(page.getByText(`${NODE_COUNT}/${NODE_COUNT}`)).toBeVisible();
  await expect(page.getByText("10,000/10,000")).toBeVisible();
  await expect(page.locator(".react-flow__node")).toHaveCount(NODE_COUNT);

  const repository = page.getByRole("button", { name: "repository repo-00" });
  await repository.focus();
  await repository.press("Enter");
  await expect(page.getByRole("heading", { name: "repo-00" })).toBeVisible();
  await expect(page.locator(".react-flow__edge-text")).toHaveCount(
    DISTINCT_PAIR_COUNT,
  );
  expect(pageErrors).toEqual([]);
});

test("keeps a 5,000-node dependency chain stack safe", async ({ page }) => {
  test.setTimeout(60_000);
  const layoutTimeout = 30_000;
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(dependencyTopologyPayload(DEEP_NODE_COUNT)),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(
    page.getByText(`${DEEP_NODE_COUNT}/${DEEP_NODE_COUNT}`, { exact: true }),
  ).toBeVisible({ timeout: layoutTimeout });
  await expect(page.locator(".react-flow__node")).toHaveCount(DEEP_NODE_COUNT, {
    timeout: layoutTimeout,
  });
  await expect(page.locator(".react-flow__edge")).toHaveCount(600, {
    timeout: layoutTimeout,
  });
  await expect(page.locator(".react-flow__edge-text")).toHaveCount(0, {
    timeout: layoutTimeout,
  });
  expect(pageErrors).toEqual([]);
});

test("keeps cyclic topology layout stack safe", async ({ page }) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(dependencyTopologyPayload(12, true)),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(page.getByText("12/12", { exact: true }).first()).toBeVisible();
  await expect(page.locator(".react-flow__node")).toHaveCount(12);
  expect(pageErrors).toEqual([]);
});

test("keeps grouped relationship semantics visible and accessible", async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(groupedSemanticsTopologyPayload()),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  const expected = [
    {
      label: "depends on ×2",
      ariaLabel:
        "depends on relationship from repository:source to repository:target, 2 grouped relationships",
      color: "rgb(22, 163, 74)",
    },
    {
      label: "candidate dependency ×2",
      ariaLabel:
        "candidate dependency relationship from repository:source to repository:target, 2 grouped relationships",
      color: "rgb(234, 88, 12)",
    },
    {
      label: "not resolved ×2",
      ariaLabel:
        "not resolved relationship from repository:source to repository:target, 2 grouped relationships",
      color: "rgb(234, 179, 8)",
    },
    {
      label: "conflicts with ×2",
      ariaLabel:
        "conflicts with relationship from repository:source to repository:target, 2 grouped relationships",
      color: "rgb(239, 68, 68)",
    },
    {
      label: "contains ×2",
      ariaLabel:
        "contains relationship from repository:source to repository:target, 2 grouped relationships",
      color: "rgb(100, 116, 139)",
    },
  ];
  for (const edge of expected) {
    await expect(page.getByText(edge.label, { exact: true })).toBeVisible();
  }
  await expect(page.locator(".react-flow__edge")).toHaveCount(expected.length);
  expect(
    await page.locator(".react-flow__edge").evaluateAll((edges) =>
      edges.map((edge) => ({
        ariaLabel: edge.getAttribute("aria-label"),
        color: getComputedStyle(
          edge.querySelector(".react-flow__edge-path") as SVGPathElement,
        ).stroke,
      })),
    ),
  ).toEqual(expected.map(({ ariaLabel, color }) => ({ ariaLabel, color })));
  expect(pageErrors).toEqual([]);
});

test("paginates accessible relationships and keeps source observations distinct", async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(accessibleTopologyPayload()),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(
    page.getByText("API response relationship list truncated", {
      exact: true,
    }),
  ).toBeVisible();
  await page.getByText("Relationship list", { exact: true }).click();
  await expect(
    page.getByText("Showing rows 1–100 of 501 returned relationships.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("accessible.ts:1", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("accessible.ts:101", { exact: true }),
  ).not.toBeAttached();

  const nextPage = page.getByRole("button", {
    name: "Next relationship page",
  });
  for (let pageNumber = 0; pageNumber < 5; pageNumber += 1) {
    await nextPage.click();
  }
  await expect(
    page.getByText("Showing rows 501–501 of 501 returned relationships.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("accessible.ts:501", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "repository source" }).click();
  const details = page.getByLabel("Topology details");
  await expect(
    details.getByText("indexed observation", { exact: true }),
  ).toBeVisible();
  await expect(
    details.getByText("main · indexed-commit", { exact: true }),
  ).toBeVisible();
  await expect(details.getByText("clean", { exact: true })).toBeVisible();
  await expect(
    details.getByText("current observation", { exact: true }),
  ).toBeVisible();
  await expect(
    details.getByText("feature/accessibility · current-commit", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(details.getByText("dirty", { exact: true })).toBeVisible();
  expect(pageErrors).toEqual([]);
});

test("keeps overlay and shared-input invalidation semantics visible", async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(overlayInvalidationTopologyPayload()),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(
    page.locator(".react-flow__edge-text", { hasText: "overlays" }),
  ).toHaveCount(1);
  await expect(
    page.locator(".react-flow__edge-text", { hasText: "invalidates" }),
  ).toHaveCount(2);
  await expect(page.getByText("violet arrow · worktree overlay")).toBeVisible();
  await expect(
    page.getByText("blue arrow · invalidates stale generation"),
  ).toBeVisible();
  await page.getByText("Relationship list", { exact: true }).click();
  const relationshipList = page.getByRole("table", {
    name: "Visible topology relationships and evidence",
  });
  await expect(
    relationshipList.getByText("feature · 000009", { exact: true }),
  ).toBeVisible();
  await expect(
    relationshipList.getByText("overlays", { exact: true }),
  ).toBeVisible();
  await expect(
    relationshipList.getByText("invalidates", { exact: true }),
  ).toHaveCount(2);
  await expect(
    relationshipList.getByText("SOURCE_INVALIDATION", { exact: true }),
  ).toHaveCount(2);
  expect(pageErrors).toEqual([]);
});

test("keeps the current map pinned until the reader loads a newer generation", async ({
  page,
}) => {
  const topologyRequests: string[] = [];
  let latestGeneration = "000107";
  await page.route("**/api/v1/meta", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ status: "ready", counts: {} }),
    });
  });
  await page.route("**/api/v1/topology**", async (route) => {
    const requestURL = new URL(route.request().url());
    topologyRequests.push(requestURL.search);
    if (requestURL.searchParams.get("generation") === "default:000107") {
      await route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: {
            code: "GENERATION_CHANGED",
            message: "refresh the selected profile",
          },
        }),
      });
      return;
    }
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(largeTopologyPayload(latestGeneration)),
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();
  await expect(page.getByText("default 000107")).toBeVisible();

  await page.getByRole("button", { name: "check for update" }).click();
  await expect(
    page.getByText(
      "A newer generation was published. This map remains pinned to the generation already shown.",
    ),
  ).toBeVisible();
  await expect(page.getByText("10,000/10,000")).toBeVisible();
  expect(topologyRequests).toContain("?profile=*");
  expect(topologyRequests).toContain(
    "?profile=*&generation=default%3A000107",
  );

  latestGeneration = "000108";
  await page.getByRole("button", { name: "load latest" }).click();
  await expect(page.getByText("default 000108")).toBeVisible();
});
