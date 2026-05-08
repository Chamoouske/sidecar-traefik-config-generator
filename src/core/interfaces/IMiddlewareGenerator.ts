import { DiscoveredService, MiddlewareConfigOutput } from '../../types/index.js';

/**
 * Interface para geração de middlewares Traefik baseados em configurações de serviço.
 */
export interface IMiddlewareGenerator {
    /**
     * Gera configuração de middlewares para um serviço descoberto.
     * @param service - Serviço descoberto
     * @returns Configuração de middlewares ou null se nenhum middleware for necessário
     */
    generate(service: DiscoveredService): Promise<MiddlewareConfigOutput | null>;
}
