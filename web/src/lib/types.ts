import type { Platform } from './format';

/** System role: admins manage everything, users get access through project membership. */
export type Role = 'admin' | 'user';

/** Role inside a project; "" means no membership (a public project can still be read). */
export type ProjectRole = 'read' | 'write' | 'admin' | '';

export interface User {
	id: number;
	username: string;
	role: Role;
	email?: string;
	groups?: string[];
	sso?: boolean;
	hasCliSecret?: boolean;
	hasPassword?: boolean;
	createdAt: number;
	lastLoginAt?: number;
}

export interface Me {
	user: User;
	version: string;
	/** Projects this user may manage (members, robots, webhooks, settings). */
	managedProjects?: string[];
}

export interface Stats {
	repositories: number;
	tags: number;
	manifests: number;
	blobs: number;
	blobBytes: number;
	uploads: number;
	disk?: { total: number; free: number };
	lastGc?: GcRun;
	nextGc: number;
	gcRunning: boolean;
}

export interface RepoListItem {
	name: string;
	project?: string;
	public: boolean;
	tags: number;
	manifests: number;
	size: number;
	lastPush: number;
	createdAt: number;
}

export interface PlatformInfo extends Platform {
	digest: string;
	size: number;
}

export interface ManifestSummary {
	digest: string;
	mediaType: string;
	artifactType?: string;
	size: number;
	platforms: PlatformInfo[];
	attestations: number;
	created?: string;
	createdAt: number;
	lastPulledAt: number;
	isReferrer: boolean;
}

export interface Repo {
	name: string;
	public: boolean;
	createdAt: number;
	size: number;
	tags: { name: string; updatedAt: number; manifest: ManifestSummary }[];
	untagged: ManifestSummary[];
}

export interface Descriptor {
	mediaType: string;
	digest: string;
	size: number;
}

/** Docker HEALTHCHECK; durations in nanoseconds, 0 means the default. */
export interface Healthcheck {
	Test?: string[];
	Interval?: number;
	Timeout?: number;
	StartPeriod?: number;
	StartInterval?: number;
	Retries?: number;
}

export interface ImageConfig {
	architecture: string;
	os: string;
	variant?: string;
	created?: string;
	author?: string;
	config: {
		User?: string;
		Env?: string[];
		Entrypoint?: string[];
		Cmd?: string[];
		WorkingDir?: string;
		Labels?: Record<string, string>;
		ExposedPorts?: Record<string, object>;
		Volumes?: Record<string, object>;
		StopSignal?: string;
		Healthcheck?: Healthcheck;
		Shell?: string[];
		OnBuild?: string[];
	};
	history?: { created?: string; created_by?: string; comment?: string; empty_layer?: boolean }[];
}

export interface ManifestDetail {
	repo: string;
	digest: string;
	mediaType: string;
	artifactType: string;
	manifestSize: number;
	raw: unknown;
	createdAt: number;
	lastPulledAt: number;
	subject: string;
	annotations?: Record<string, string>;
	summary: ManifestSummary;
	tags: string[];
	parents: string[];
	referrers: ManifestSummary[];
	children?: {
		digest: string;
		mediaType: string;
		size: number;
		platform?: Platform & { 'os.version'?: string };
		attestation: boolean;
		imageSize: number;
	}[];
	layers?: (Descriptor & { external?: boolean })[];
	configDescriptor?: Descriptor;
	config?: ImageConfig | null;
}

export interface GcSettings {
	intervalHours: number;
	/** Days to keep audit log entries; 0 keeps them forever. */
	auditRetentionDays?: number;
	graceMinutes: number;
	deleteUntagged: boolean;
	keepPulledUntaggedDays: number;
	uploadMaxAgeHours: number;
	applyRetention: boolean;
}

export interface TagDeletion {
	repo: string;
	tag: string;
	digest: string;
	updatedAt: number;
	policy: string;
}

export interface GcReport {
	deletedTags: TagDeletion[];
	deletedManifests: { repo: string; digest: string; mediaType: string; createdAt: number }[];
	deletedRepos: string[];
	keptRecent: number;
	unlinkedBlobs: number;
	deletedBlobs: number;
	freedBytes: number;
	orphanFiles: number;
	staleUploads: number;
	prunedAuditEntries?: number;
}

export interface GcRun {
	id: number;
	trigger: string;
	dryRun: boolean;
	startedAt: number;
	finishedAt: number;
	status: string;
	deletedTags: number;
	deletedManifests: number;
	deletedBlobs: number;
	freedBytes: number;
	error?: string;
	report?: GcReport;
}

export interface Policy {
	id: number;
	name: string;
	repoPattern: string;
	tagPattern: string;
	keepPattern: string;
	keepLast: number;
	olderThanDays: number;
	enabled: boolean;
	createdAt?: number;
}

/** GET /api/auth/config; a 404 means the server has no OIDC support. */
export interface AuthConfig {
	oidc: boolean;
	oidcName?: string;
	/** False when only single sign-on is allowed. */
	localLogin?: boolean;
	/** Visitors without a session may browse public repositories. */
	publicBrowse?: boolean;
}

export interface Project {
	id: number;
	name: string;
	public: boolean;
	/** 0 means no quota. */
	quotaBytes: number;
	createdAt: number;
	/** The caller's effective role. */
	role: ProjectRole;
	repositories: number;
	usedBytes: number;
}

export interface Member {
	id: number;
	userId?: number;
	username?: string;
	group?: string;
	role: Exclude<ProjectRole, ''>;
	createdAt: number;
}

export interface ProjectDetail {
	project: Project;
	/** Only present for callers who manage the project. */
	members?: Member[];
}

export interface Robot {
	id: number;
	/** Full login name, e.g. robot$team+ci. */
	name: string;
	description: string;
	/** Empty for system robots that reach every project. */
	project?: string;
	canPush: boolean;
	canDelete: boolean;
	/** Unix seconds; 0 never expires. */
	expiresAt: number;
	disabled: boolean;
	createdBy: string;
	createdAt: number;
	lastUsedAt: number;
}

export interface RobotWithSecret {
	robot: Robot;
	secret: string;
}

export type ActorKind = 'user' | 'robot' | 'anonymous' | 'system';

export interface AuditEntry {
	id: number;
	at: number;
	actor: string;
	actorKind: ActorKind;
	action: string;
	project: string;
	resource: string;
	detail: string;
	ip: string;
}

export interface Webhook {
	id: number;
	project?: string;
	name: string;
	url: string;
	hasSecret: boolean;
	events: string[];
	enabled: boolean;
	createdAt: number;
	lastAt: number;
	lastStatus: number;
	lastError: string;
}

export interface WebhookList {
	webhooks: Webhook[];
	events: string[];
}

export interface Delivery {
	id: number;
	event: string;
	at: number;
	attempt: number;
	statusCode: number;
	error: string;
	durationMs: number;
	payload: string;
}

export type DeviceStatus = 'pending' | 'approved' | 'denied' | 'expired';

/** A `doreg login` request waiting for the signed-in user to confirm it in the browser. */
export interface DeviceRequest {
	userCode: string;
	/** The requesting machine, e.g. "macbook (darwin)". */
	client: string;
	createdAt: number;
	expiresAt: number;
	status: DeviceStatus;
}

/** A token issued to the CLI through `doreg login`. */
export interface CliToken {
	id: number;
	name: string;
	createdAt: number;
	/** Unix seconds; 0 never expires. */
	expiresAt: number;
	lastUsedAt: number;
}

/** A repository the import found in the source registry. */
export interface ImportRepo {
	name: string;
	tags: string[];
}

export interface ImportProgress {
	repositories: number;
	tags: number;
	tagsCopied: number;
	tagsSkipped: number;
	tagsFailed: number;
	blobs: number;
	bytes: number;
	/** The first failures, as "repo:tag: message". */
	errors: string[];
}

/** The current or last import from another registry. */
export interface ImportJob {
	source: string;
	actor: string;
	status: 'running' | 'done' | 'failed' | 'canceled';
	startedAt: number;
	finishedAt?: number;
	error?: string;
	progress: ImportProgress;
}
