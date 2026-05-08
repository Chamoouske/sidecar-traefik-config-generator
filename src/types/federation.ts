/**
 * Tipos de Federação e Geração de Configuração Traefik.
 */

export interface ServerDefinition {
    url: string;
    weight?: number;
}

export interface RetryConfig {
    attempts: number;
    initialInterval: string;
}

export interface CircuitBreakerConfig {
    expression: string;
}

export interface HealthCheckConfig {
    path: string;
    interval: string;
}

export interface StickyConfig {
    cookie: Record<string, string>;
}

export interface LoadBalancerConfig {
    passHostHeader: boolean;
    servers: ServerDefinition[];
    healthCheck?: HealthCheckConfig;
    sticky?: StickyConfig;
}

export interface ServiceOutput {
    loadBalancer: LoadBalancerConfig;
    circuitBreaker?: CircuitBreakerConfig;
}

export interface RouterOutput {
    rule: string;
    service: string;
    entryPoints: string[];
    middlewares?: string[];
    priority?: number;
}

export interface MiddlewareOutput {
    retry?: RetryConfig;
    circuitBreaker?: CircuitBreakerConfig;
    headers?: Record<string, unknown>;
}

export interface FederationConfigOutput {
    http: {
        services: Record<string, ServiceOutput>;
    };
}

export interface LocalConfigOutput {
    http: {
        services: Record<string, ServiceOutput>;
        routers: Record<string, RouterOutput>;
    };
}

export interface MiddlewareConfigOutput {
    http: {
        middlewares: Record<string, MiddlewareOutput>;
    };
}

export type GenerationResult = {
    federation?: FederationConfigOutput;
    local?: LocalConfigOutput;
    middlewares?: MiddlewareConfigOutput;
};
