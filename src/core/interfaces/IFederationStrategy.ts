import { DiscoveredService, FederationConfigOutput } from '../../types/index.js';

/**
 * Interface para estratégias de federação entre nós do Swarm.
 * Diferentes implementações podem lidar com diferentes padrões de
 * descoberta e configuração de serviços federados.
 */
export interface IFederationStrategy {
    /**
     * Verifica se esta estratégia pode processar o serviço.
     * @param service - Serviço descoberto a ser avaliado
     * @returns true se a estratégia for aplicável
     */
    canHandle(service: DiscoveredService): boolean;

    /**
     * Gera a configuração de federação para o serviço.
     * @param service - Serviço descoberto para gerar configuração
     * @returns Configuração de federação Traefik
     */
    generate(service: DiscoveredService): Promise<FederationConfigOutput>;
}
