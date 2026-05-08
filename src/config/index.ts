/**
 * AppConfigService - Carregamento e validação de configuração do sidecar.
 *
 * Implementa a interface {@link IConfig} carregando valores de variáveis
 * de ambiente com defaults sensíveis para operação em Docker Swarm.
 */

import * as os from 'node:os';
import { IConfig } from '../core/interfaces/IConfig.js';
import { GenerationMode, AppConfig, EnvVars } from '../types/config.js';
import { ConfigValidationError, InvalidModeError } from '../types/errors.js';

function parseGenerationMode(raw?: string): GenerationMode {
    if (!raw || raw === 'all') return 'all';
    if (raw === 'global') return 'global';
    if (raw === 'local') return 'local';
    throw new InvalidModeError(raw!);
}

export class AppConfigService implements IConfig {
    private config!: AppConfig;

    constructor(private readonly env: EnvVars) { }

    /**
     * Carrega a configuração a partir das variáveis de ambiente fornecidas,
     * aplicando defaults para valores não preenchidos.
     *
     * @returns A configuração completa do sidecar
     */
    load(): AppConfig {
        const mode = parseGenerationMode(this.env.GENERATION_MODE);
        const hostname = this.env.NODE_HOSTNAME || os.hostname();
        const nodeIp = this.env.NODE_IP || '127.0.0.1';
        const nodeId = this.env.NODE_ID || 'unknown';
        const dockerSocket =
            this.env.DOCKER_SOCKET ||
            (process.platform === 'win32'
                ? '//./pipe/docker_engine'
                : '/var/run/docker.sock');
        const pollIntervalMs = parseInt(
            this.env.POLL_INTERVAL_MS || '30000',
            10,
        );
        const sharedDir = this.env.SHARED_DIR || '/data/shared';
        const localDir = this.env.LOCAL_DIR || '/data/local';
        const serverPort = parseInt(this.env.SERVER_PORT || '9090', 10);
        const healthEndpoint = this.env.HEALTH_ENDPOINT || '/health';
        const logLevel = this.env.LOG_LEVEL || 'info';
        const logPretty = (this.env.LOG_PRETTY || 'false') === 'true';
        const headerName =
            this.env.FEDERATION_HEADER_NAME || 'X-Federated';
        const headerValue =
            this.env.FEDERATION_HEADER_VALUE || 'true';
        const circuitBreakerThreshold = parseFloat(
            this.env.CIRCUIT_BREAKER_THRESHOLD || '0.30',
        );
        const defaultRetryAttempts = parseInt(
            this.env.DEFAULT_RETRY_ATTEMPTS || '3',
            10,
        );
        const defaultRetryInterval =
            this.env.DEFAULT_RETRY_INTERVAL || '100ms';

        this.config = {
            mode,
            node: {
                hostname,
                ip: nodeIp,
                nodeId,
            },
            docker: {
                socket: dockerSocket,
                pollIntervalMs: Number.isNaN(pollIntervalMs)
                    ? 30000
                    : pollIntervalMs,
            },
            directories: {
                shared: sharedDir,
                local: localDir,
                federation: `${sharedDir}/federation`,
                middlewares: `${sharedDir}/middlewares`,
                localGenerated: `${localDir}/generated`,
            },
            federation: {
                headerName,
                headerValue,
                defaultHealthCheckPath: '/',
                defaultHealthCheckInterval: '10s',
                defaultRetryAttempts: Number.isNaN(defaultRetryAttempts)
                    ? 3
                    : defaultRetryAttempts,
                defaultRetryInterval,
                circuitBreakerThreshold: Number.isNaN(
                    circuitBreakerThreshold,
                )
                    ? 0.3
                    : circuitBreakerThreshold,
            },
            server: {
                port: Number.isNaN(serverPort) ? 9090 : serverPort,
                healthEndpoint,
            },
            logging: {
                level: logLevel,
                pretty: logPretty,
            },
        };

        return this.config;
    }

    /**
     * Valida a configuração carregada, lançando {@link ConfigValidationError}
     * se algum valor estiver fora dos limites aceitáveis.
     *
     * Regras de validação:
     * - Diretórios shared e local não podem ser iguais
     * - Poll interval >= 1000ms
     * - Porta do servidor entre 1 e 65535
     * - Circuit breaker threshold entre 0 e 1
     * - Retry attempts >= 0
     */
    validate(): void {
        if (this.config.directories.shared === this.config.directories.local) {
            throw new ConfigValidationError(
                'Shared and local directories must be different',
            );
        }

        if (this.config.docker.pollIntervalMs < 1000) {
            throw new ConfigValidationError(
                `Poll interval must be >= 1000ms, got ${this.config.docker.pollIntervalMs}`,
            );
        }

        const port = this.config.server.port;
        if (Number.isNaN(port) || port <= 0 || port >= 65536) {
            throw new ConfigValidationError(
                `Port must be > 0 and < 65536, got ${port}`,
            );
        }

        const threshold = this.config.federation.circuitBreakerThreshold;
        if (threshold < 0 || threshold > 1) {
            throw new ConfigValidationError(
                `Circuit breaker threshold must be between 0 and 1, got ${threshold}`,
            );
        }

        if (this.config.federation.defaultRetryAttempts < 0) {
            throw new ConfigValidationError(
                `Retry attempts must be >= 0, got ${this.config.federation.defaultRetryAttempts}`,
            );
        }
    }

    /**
     * Retorna a configuração atualmente carregada.
     * @throws {Error} Se `load()` não tiver sido chamado antes
     */
    get(): AppConfig {
        if (!this.config) {
            throw new Error(
                'Config not loaded. Call load() before get().',
            );
        }
        return this.config;
    }
}
