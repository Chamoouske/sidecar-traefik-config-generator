/**
 * Barrel export para todos os tipos do sidecar.
 */

export type {
    SwarmNode,
    SwarmTask,
    SwarmService,
    ServiceEndpoint,
    DiscoveredService,
} from './docker.js';

export type {
    AppConfig,
    LabelConfig,
    EnvVars,
} from './config.js';

export type {
    ServerDefinition,
    RetryConfig,
    CircuitBreakerConfig,
    HealthCheckConfig,
    StickyConfig,
    LoadBalancerConfig,
    ServiceOutput,
    RouterOutput,
    MiddlewareOutput,
    FederationConfigOutput,
    LocalConfigOutput,
    MiddlewareConfigOutput,
    GenerationResult,
} from './federation.js';

export {
    SidecarError,
    DockerConnectionError,
    ConfigValidationError,
    FileWriteError,
    DiscoveryError,
} from './errors.js';
