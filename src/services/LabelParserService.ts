/**
 * LabelParserService - Parsing de labels Docker para configuração de federação.
 *
 * Interpreta as labels no formato `federation.*` aplicadas aos serviços
 * Docker Swarm para extrair configurações de:
 * - Habilitação de federação (`federation.enable`)
 * - Host e porta do serviço
 * - Sticky sessions, retry, circuit breaker
 * - Health check e locality-aware routing
 */

import { ILabelParser } from '../core/interfaces/ILabelParser.js';
import { LabelConfig } from '../types/config.js';

/**
 * Prefixo usado nas labels de federação.
 */
const LABEL_PREFIX = 'federation';

/**
 * Chaves de labels reconhecidas pelo parser.
 */
const LABEL_KEYS = {
    ENABLE: `${LABEL_PREFIX}.enable`,
    HOST: `${LABEL_PREFIX}.host`,
    PORT: `${LABEL_PREFIX}.port`,
    STICKY: `${LABEL_PREFIX}.sticky`,
    RETRY_ATTEMPTS: `${LABEL_PREFIX}.retryAttempts`,
    RETRY_INTERVAL: `${LABEL_PREFIX}.retryInterval`,
    CIRCUIT_BREAKER: `${LABEL_PREFIX}.circuitBreaker`,
    HEALTH_CHECK_PATH: `${LABEL_PREFIX}.healthCheckPath`,
    HEALTH_CHECK_INTERVAL: `${LABEL_PREFIX}.healthCheckInterval`,
    LOCALITY_AWARE: `${LABEL_PREFIX}.localityAware`,
} as const;

export class LabelParserService implements ILabelParser {
    /**
     * Analisa as labels de um serviço e retorna a configuração de federação.
     *
     * Regras de parsing:
     * - `federation.enable=true` é obrigatório para ser um serviço federado
     * - `federation.host` é obrigatório se enabled
     * - `federation.port` é obrigatório se enabled
     * - Opcionais têm defaults sensíveis
     *
     * @param labels - Mapa de chave/valor das labels do serviço Docker
     * @returns Configuração extraída ou `null` se o serviço não for federado
     *          ou se as labels obrigatórias estiverem ausentes
     */
    parse(labels: Record<string, string>): LabelConfig | null {
        const enabled = labels[LABEL_KEYS.ENABLE] === 'true';
        if (!enabled) return null;

        const host = labels[LABEL_KEYS.HOST];
        const portStr = labels[LABEL_KEYS.PORT];
        const port = portStr ? parseInt(portStr, 10) : 0;

        // Se está enabled, host e port são obrigatórios
        if (!host || !portStr) return null;
        if (isNaN(port) || port <= 0 || port >= 65536) return null;

        return {
            enabled: true,
            host,
            port,
            sticky: labels[LABEL_KEYS.STICKY] === 'true',
            retryAttempts: labels[LABEL_KEYS.RETRY_ATTEMPTS]
                ? parseInt(labels[LABEL_KEYS.RETRY_ATTEMPTS], 10)
                : 3,
            retryInterval:
                labels[LABEL_KEYS.RETRY_INTERVAL] || '100ms',
            circuitBreaker:
                labels[LABEL_KEYS.CIRCUIT_BREAKER] === 'true',
            healthCheckPath:
                labels[LABEL_KEYS.HEALTH_CHECK_PATH] || '/',
            healthCheckInterval:
                labels[LABEL_KEYS.HEALTH_CHECK_INTERVAL] || '10s',
            localityAware:
                labels[LABEL_KEYS.LOCALITY_AWARE] === 'true',
        };
    }

    /**
     * Verifica se as labels indicam que o serviço tem federação habilitada.
     *
     * @param labels - Mapa de chave/valor das labels do serviço
     * @returns `true` se `federation.enable=true`
     */
    isFederationEnabled(labels: Record<string, string>): boolean {
        return labels[LABEL_KEYS.ENABLE] === 'true';
    }
}
