import { expect, test } from "@playwright/test";

const REPOSITORY_COUNT = 53;
const RELATIONSHIP_COUNT = 10_000;

function repositoryID(index: number): string {
  return `repo-${String(index).padStart(2, "0")}`;
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

test("keeps a 107-node, 10,000-relationship topology explorable", async ({
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
      body: JSON.stringify(largeTopologyPayload()),
    });
  });

  const startedAt = performance.now();
  await page.goto("/");
  await page.getByRole("button", { name: "topology" }).click();

  await expect(page.getByTestId("topology-explorer")).toBeVisible();
  await expect(page.getByText("107/107")).toBeVisible();
  await expect(page.getByText("10,000/10,000")).toBeVisible();
  await expect(page.locator(".react-flow__node")).toHaveCount(107);
  expect(performance.now() - startedAt).toBeLessThan(8_000);

  const repository = page.getByRole("button", { name: "repository repo-00" });
  await repository.focus();
  await repository.press("Enter");
  await expect(page.getByRole("heading", { name: "repo-00" })).toBeVisible();
  await expect(page.locator(".react-flow__edge-text")).toHaveCount(52);
  expect(pageErrors).toEqual([]);
});

test("keeps the current map pinned until the reader loads a newer generation", async ({
  page,
}) => {
  const topologyRequests: string[] = [];
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
      body: JSON.stringify(
        largeTopologyPayload(
          topologyRequests.length === 1 ? "000107" : "000108",
        ),
      ),
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
  expect(topologyRequests).toContain("?generation=default%3A000107");

  await page.getByRole("button", { name: "load latest" }).click();
  await expect(page.getByText("default 000108")).toBeVisible();
});
