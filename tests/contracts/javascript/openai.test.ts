import { test } from "bun:test";
import OpenAI from "openai";

const MODEL = "gpt-5";
const REQUEST_TIMEOUT_MS = 5_000;

type JSONValue = Record<string, unknown>;

class ContractFailure extends Error {
  constructor(stage: string, detail: string) {
    super(`stage=${stage} ${detail}`);
  }
}

class AdminSession {
  readonly origin: string;
  #cookie = "";

  constructor(origin: string) {
    this.origin = origin;
  }

  async request<T extends JSONValue>(
    method: string,
    path: string,
    stage: string,
    expectedStatus: number,
    body?: JSONValue,
    decode = true,
  ): Promise<T | undefined> {
    const headers = new Headers({ Accept: "application/json" });
    if (body !== undefined) headers.set("Content-Type", "application/json");
    if (this.#cookie) headers.set("Cookie", this.#cookie);

    let response: Response;
    try {
      response = await fetch(`${this.origin}${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });
    } catch {
      throw new ContractFailure(stage, "failure=send_request");
    }

    if (stage === "admin_login") {
      const setCookie = response.headers.get("set-cookie");
      const cookie = setCookie?.split(";", 1)[0]?.trim() ?? "";
      if (!cookie) throw new ContractFailure(stage, "field=session_cookie");
      this.#cookie = cookie;
    }

    if (response.status !== expectedStatus) {
      try {
        await response.arrayBuffer();
      } catch {
        // Cleanup and error reporting never retain response bodies.
      }
      throw new ContractFailure(stage, `status=${response.status} expected=${expectedStatus}`);
    }
    if (!decode) {
      try {
        await response.arrayBuffer();
      } catch {
        // A successful empty response needs no diagnostic body.
      }
      return undefined;
    }

    try {
      return (await response.json()) as T;
    } catch {
      throw new ContractFailure(stage, "failure=decode_response");
    }
  }

  async quiet(method: string, path: string, expectedStatus?: number): Promise<void> {
    try {
      await this.request(
        method,
        path,
        "cleanup",
        expectedStatus ?? (method === "POST" ? 200 : 204),
        undefined,
        false,
      );
    } catch {
      // Cleanup is best effort and must never print credentials or bodies.
    }
  }

  clear(): void {
    this.#cookie = "";
  }
}

type FixtureResources = {
  accountID: number;
  poolID: number;
  keyID: number;
  clientSecret: string;
};

class ContractFixture {
  readonly admin: AdminSession;
  readonly sdkBaseURL: string;
  readonly adminUsername: string;
  readonly adminPassword: string;
  readonly mockBaseURL: string;
  readonly mockAPIKey: string;
  readonly suffix: string;
  readonly resources: FixtureResources = {
    accountID: 0,
    poolID: 0,
    keyID: 0,
    clientSecret: "",
  };

  private constructor(
    gatewayURL: URL,
    adminUsername: string,
    adminPassword: string,
    mockBaseURL: string,
    mockAPIKey: string,
  ) {
    this.admin = new AdminSession(gatewayURL.origin);
    this.sdkBaseURL = `${gatewayURL.origin}/v1`;
    this.adminUsername = adminUsername;
    this.adminPassword = adminPassword;
    this.mockBaseURL = mockBaseURL;
    this.mockAPIKey = mockAPIKey;
    this.suffix = crypto.randomUUID().replaceAll("-", "").slice(0, 16);
  }

  static fromEnvironment(): ContractFixture {
    const required = [
      "N2API_CONTRACT_BASE_URL",
      "N2API_CONTRACT_ADMIN_USERNAME",
      "N2API_CONTRACT_ADMIN_PASSWORD",
      "N2API_CONTRACT_MOCK_BASE_URL",
      "N2API_CONTRACT_MOCK_API_KEY",
    ] as const;
    const values = new Map<string, string>();
    for (const name of required) {
      const value = process.env[name]?.trim() ?? "";
      if (!value) throw new ContractFailure("config", `missing=${name}`);
      values.set(name, value);
    }

    let gatewayURL: URL;
    let mockURL: URL;
    try {
      gatewayURL = new URL(values.get("N2API_CONTRACT_BASE_URL")!);
      mockURL = new URL(values.get("N2API_CONTRACT_MOCK_BASE_URL")!);
    } catch {
      throw new ContractFailure("config", "field=url");
    }
    if (gatewayURL.protocol !== "http:" || gatewayURL.hostname !== "n2api") {
      throw new ContractFailure("config", "field=gateway_host");
    }
    if (mockURL.protocol !== "http:" || mockURL.hostname !== "mock-openai") {
      throw new ContractFailure("config", "field=mock_host");
    }

    return new ContractFixture(
      gatewayURL,
      values.get("N2API_CONTRACT_ADMIN_USERNAME")!,
      values.get("N2API_CONTRACT_ADMIN_PASSWORD")!,
      mockURL.origin,
      values.get("N2API_CONTRACT_MOCK_API_KEY")!,
    );
  }

  async provision(): Promise<void> {
    await this.admin.request(
      "POST",
      "/api/admin/login",
      "admin_login",
      200,
      { username: this.adminUsername, password: this.adminPassword },
    );

    const account = await this.admin.request<{
      account: { id: number };
    }>("POST", "/api/admin/provider-accounts/api-upstream", "account_create", 201, {
      name: `SDK JavaScript upstream ${this.suffix}`,
      baseUrl: this.mockBaseURL,
      apiKey: this.mockAPIKey,
      enabled: true,
      priority: 0,
      loadFactor: 1,
      models: [MODEL],
    });
    this.resources.accountID = account?.account.id ?? 0;
    if (this.resources.accountID <= 0) {
      throw new ContractFailure("account_create", "field=id");
    }

    const pool = await this.admin.request<{ pool: { id: number } }>(
      "POST",
      "/api/admin/routing-pools",
      "pool_create",
      201,
      {
        name: `sdk-javascript-${this.suffix}`,
        description: "OpenAI JavaScript SDK contract",
        enabled: true,
      },
    );
    this.resources.poolID = pool?.pool.id ?? 0;
    if (this.resources.poolID <= 0) {
      throw new ContractFailure("pool_create", "field=id");
    }

    await this.admin.request(
      "PUT",
      `/api/admin/routing-pools/${this.resources.poolID}/accounts`,
      "pool_membership",
      200,
      { accounts: [{ accountId: this.resources.accountID, priority: 0 }] },
    );

    const key = await this.admin.request<{
      key: { id: number };
      secret: string;
    }>("POST", "/api/admin/keys", "client_key_create", 201, {
      name: `sdk-javascript-${this.suffix}`,
      routingPoolId: this.resources.poolID,
    });
    this.resources.keyID = key?.key.id ?? 0;
    this.resources.clientSecret = key?.secret ?? "";
    if (this.resources.keyID <= 0 || !this.resources.clientSecret) {
      throw new ContractFailure("client_key_create", "field=credentials");
    }
  }

  async cleanup(): Promise<void> {
    if (this.resources.keyID > 0) {
      await this.admin.quiet("POST", `/api/admin/keys/${this.resources.keyID}/revoke`);
      await this.admin.quiet("DELETE", `/api/admin/keys/${this.resources.keyID}`);
    }
    if (this.resources.poolID > 0) {
      await this.admin.quiet("DELETE", `/api/admin/routing-pools/${this.resources.poolID}`);
    }
    if (this.resources.accountID > 0) {
      await this.admin.quiet("DELETE", `/api/admin/provider-accounts/${this.resources.accountID}`);
    }
    await this.admin.quiet("POST", "/api/admin/logout", 204);
    this.resources.clientSecret = "";
    this.admin.clear();
  }
}

async function sdkStage<T>(stage: string, action: () => Promise<T>): Promise<T> {
  try {
    return await action();
  } catch (error) {
    if (error instanceof ContractFailure) throw error;
    throw new ContractFailure(stage, "failure=sdk_request");
  }
}

function requireContract(condition: boolean, stage: string, field: string): void {
  if (!condition) throw new ContractFailure(stage, `field=${field}`);
}

test("official OpenAI JavaScript SDK matches the N2API contract", async () => {
  const fixture = ContractFixture.fromEnvironment();
  let succeeded = false;
  try {
    await fixture.provision();
    const client = new OpenAI({
      baseURL: fixture.sdkBaseURL,
      apiKey: fixture.resources.clientSecret,
      maxRetries: 0,
      timeout: REQUEST_TIMEOUT_MS,
    });

    const models = await sdkStage("models_list", () => client.models.list());
    requireContract(models.data.some((model) => model.id === MODEL), "models_list", "model");

    const chat = await sdkStage("chat_json", () =>
      client.chat.completions.create({
        model: MODEL,
        messages: [{ role: "user", content: "JavaScript SDK contract request" }],
      }),
    );
    requireContract(chat.object === "chat.completion", "chat_json", "object");
    requireContract(chat.usage?.total_tokens === 25, "chat_json", "usage");

    const stream = await sdkStage("responses_stream", () =>
      client.responses.create({
        model: MODEL,
        input: "JavaScript SDK contract request",
        stream: true,
      }),
    );
    let completed = false;
    await sdkStage("responses_stream", async () => {
      for await (const event of stream) {
        if (event.type === "response.completed") completed = true;
      }
    });
    requireContract(completed, "responses_stream", "completed");

    const invalidClient = new OpenAI({
      baseURL: fixture.sdkBaseURL,
      apiKey: "n2api_invalid_contract_key",
      maxRetries: 0,
      timeout: REQUEST_TIMEOUT_MS,
    });
    let authenticationError = false;
    try {
      await invalidClient.models.list();
    } catch (error) {
      authenticationError = error instanceof OpenAI.AuthenticationError && error.status === 401;
    }
    requireContract(authenticationError, "invalid_key", "error_type");
    succeeded = true;
  } finally {
    const preserveFailureState = process.env.N2API_CONTRACT_PRESERVE_FAILURE_STATE === "true";
    if (succeeded || !preserveFailureState) await fixture.cleanup();
  }
}, 30_000);
