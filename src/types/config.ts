/**
 * Tipos de Configuração do Sidecar.
 */

export interface AppConfig {
    node: {
        hostname: string;
        ip: string;
        nodeId: string;
    };
    docker: {
        socket: string;
        pollIntervalMs: number;
    };
    directories: {
        shared: string;
        local: string;
        federation: string;
        middlewares: string;
        localGenerated: string;
    };
    federation: {
        headerName: string;
        headerValue: string;
        defaultHealthCheckPath: string;
        defaultHealthCheckInterval: string;
        defaultRetryAttempts: number;
        defaultRetryInterval: string;
        circuitBreakerThreshold: number;
    };
    server: {
        port: number;
        healthEndpoint: string;
    };
    logging: {
        level: string;
        pretty: boolean;
    };
}

export interface LabelConfig {
    enabled: boolean;
    host: string;
    port: number;
    sticky?: boolean;
    retryAttempts?: number;
    retryInterval?: string;
    circuitBreaker?: boolean;
    healthCheckPath?: string;
    healthCheckInterval?: string;
    localityAware?: boolean;
}

export interface EnvVars {
    NODE_HOSTNAME?: string;
    NODE_IP?: string;
    NODE_ID?: string;
    DOCKER_SOCKET?: string;
    POLL_INTERVAL_MS?: string;
    SHARED_DIR?: string;
    LOCAL_DIR?: string;
    SERVER_PORT?: string;
    HEALTH_ENDPOINT?: string;
    LOG_LEVEL?: string;
    LOG_PRETTY?: string;
    FEDERATION_HEADER_NAME?: string;
    FEDERATION_HEADER_VALUE?: string;
    CIRCUIT_BREAKER_THRESHOLD?: string;
    DEFAULT_RETRY_ATTEMPTS?: string;
    DEFAULT_RETRY_INTERVAL?: string;
}
