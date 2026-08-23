import { Container, getContainer } from "@cloudflare/containers";
import { env } from "cloudflare:workers";

const STATUS_URL = "http://cloud-worker/status";
const ASSIGN_URL = "http://cloud-worker/assign";
const BEARER_PREFIX = "Bearer ";
const MAX_STATUS_FAILURES = 3;

const TIER_LITE = "lite";
const TIER_STANDARD = "standard";

type StatusResponse = {
	busy?: boolean;
	running?: number;
	capacity?: number;
};

type TierRouting = {
	namespace: DurableObjectNamespace<CloudWorkerBase>;
	containers: number;
	name: string;
};

abstract class CloudWorkerBase extends Container {
	defaultPort = 8080;
	// A container that holds a job renews this window on every expiry, so it
	// only bounds how long an idle container waits for the next job.
	sleepAfter = "1m";
	enableInternet = true;
	envVars = {
		ECHOPOINT_API_BASE_URL: env.ECHOPOINT_API_BASE_URL,
		CLOUD_WORKER_MAX_JOBS: this.maxJobs(),
	};

	// Read by the field initializer above, so it is a method rather than a
	// field: a subclass field would still be undefined at that point. It is
	// public because a protected member would make the tier namespaces
	// mutually unassignable.
	abstract maxJobs(): string;

	private statusFailures = 0;
	private assignsSeen = 0;

	override async fetch(request: Request): Promise<Response> {
		this.assignsSeen++;
		return super.fetch(request);
	}

	// The probe below is a fetch, and awaiting a fetch opens the input gate, so
	// an assign can be accepted while the answer is in flight. Stopping on a
	// stale answer would kill a job the container just took, so the count is
	// read again after the probe.
	override async onActivityExpired(): Promise<void> {
		const seenBefore = this.assignsSeen;
		const busy = await this.holdsJob();
		if (busy || this.assignsSeen !== seenBefore) {
			this.renewActivityTimeout();
			return;
		}
		await this.stop();
	}

	private async holdsJob(): Promise<boolean> {
		try {
			const response = await this.containerFetch(new Request(STATUS_URL));
			if (!response.ok) {
				return this.countStatusFailure();
			}
			const status = (await response.json()) as StatusResponse;
			this.statusFailures = 0;
			return status.busy === true;
		} catch {
			return this.countStatusFailure();
		}
	}

	private countStatusFailure(): boolean {
		this.statusFailures++;
		return this.statusFailures < MAX_STATUS_FAILURES;
	}
}

// One class per size: instance_type is set on the class in wrangler.jsonc and
// cannot be chosen per instance, so a tier is a class rather than a name.
// CLOUD_WORKER_MAX_JOBS is how many jobs one container of that size runs at
// once, and the memory of the instance type is what bounds it.
export class CloudWorkerLite extends CloudWorkerBase {
	override maxJobs(): string {
		return "2";
	}
}

export class CloudWorkerStandard extends CloudWorkerBase {
	override maxJobs(): string {
		return "4";
	}
}

type AssignBody = {
	job_id?: string;
	job_token?: string;
	tier?: string;
};

export default {
	async fetch(request: Request, workerEnv: Env): Promise<Response> {
		const url = new URL(request.url);
		if (request.method !== "POST" || url.pathname !== "/assign") {
			return new Response("not found", { status: 404 });
		}

		const authorization = request.headers.get("Authorization") ?? "";
		const provided = authorization.startsWith(BEARER_PREFIX)
			? authorization.slice(BEARER_PREFIX.length)
			: undefined;
		if (!(await assignSecretMatches(provided, workerEnv.ASSIGN_SECRET))) {
			return new Response("unauthorized", { status: 401 });
		}

		let body: AssignBody;
		try {
			body = (await request.json()) as AssignBody;
		} catch {
			return new Response("invalid json", { status: 400 });
		}
		const jobID = body.job_id;
		const jobToken = body.job_token;
		if (!jobID || !jobToken) {
			return new Response("job_id and job_token are required", { status: 400 });
		}

		const routing = routingForTier(workerEnv, body.tier);
		if (!routing) {
			return new Response("tier is not supported", { status: 400 });
		}

		return assignToTier(routing, jobID, jobToken);
	},
};

// Containers hold several jobs, so a job is routed to a container of its tier
// rather than to a container of its own. Every container of the tier is tried
// once, starting at a rotating offset, and a full container answers 429.
async function assignToTier(
	routing: TierRouting,
	jobID: string,
	jobToken: string,
): Promise<Response> {
	const offset = slotOffset(jobID, routing.containers);
	let rejected: Response | undefined;
	for (let attempt = 0; attempt < routing.containers; attempt++) {
		const slot = ((offset + attempt) % routing.containers) + 1;
		const container = getContainer(routing.namespace, `${routing.name}-${slot}`);
		try {
			const assigned = await container.fetch(
				new Request(ASSIGN_URL, {
					method: "POST",
					headers: { "content-type": "application/json" },
					body: JSON.stringify({ job_id: jobID, job_token: jobToken }),
				}),
			);
			if (assigned.ok) {
				return new Response(null, { status: 202 });
			}
			if (assigned.status !== 429) {
				rejected = new Response("cloud worker did not accept the job", { status: 502 });
			}
		} catch {
			// A container that fails to start, or one refused because
			// max_instances is reached, throws rather than answering. The other
			// containers of the tier may still have room.
			rejected = new Response("cloud worker could not be started", { status: 502 });
		}
	}
	return rejected ?? new Response("every cloud worker of this tier is full", { status: 503 });
}

// The offset spreads jobs over the tier without any shared state. It is derived
// from the job id rather than random so a retry of one assign starts on the
// same container.
function slotOffset(jobID: string, containers: number): number {
	let hash = 0;
	for (let index = 0; index < jobID.length; index++) {
		hash = (hash * 31 + jobID.charCodeAt(index)) % containers;
	}
	return hash;
}

function routingForTier(workerEnv: Env, tier: string | undefined): TierRouting | undefined {
	switch (tier ?? TIER_LITE) {
		case TIER_LITE:
			return {
				namespace: workerEnv.CLOUD_WORKER_LITE,
				containers: containerCount(workerEnv.CLOUD_WORKER_LITE_CONTAINERS),
				name: TIER_LITE,
			};
		case TIER_STANDARD:
			return {
				namespace: workerEnv.CLOUD_WORKER_STANDARD,
				containers: containerCount(workerEnv.CLOUD_WORKER_STANDARD_CONTAINERS),
				name: TIER_STANDARD,
			};
		default:
			return undefined;
	}
}

function containerCount(configured: string | undefined): number {
	const parsed = Number.parseInt(configured ?? "", 10);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
}

// Both values are hashed first so the comparison is over two fixed-size
// digests. Comparing the raw bytes would need a length check, and that check
// leaks the length of the secret through timing.
async function assignSecretMatches(
	provided: string | undefined,
	expected: string | undefined,
): Promise<boolean> {
	if (!provided || !expected) {
		return false;
	}
	const encoder = new TextEncoder();
	const [left, right] = await Promise.all([
		crypto.subtle.digest("SHA-256", encoder.encode(provided)),
		crypto.subtle.digest("SHA-256", encoder.encode(expected)),
	]);
	return crypto.subtle.timingSafeEqual(left, right);
}
