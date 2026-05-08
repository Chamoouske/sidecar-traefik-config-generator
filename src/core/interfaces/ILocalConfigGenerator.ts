import { DiscoveredService } from '../../types/docker.js';
import { LocalConfigOutput } from '../../types/federation.js';

/**
 * Interface para geração de configuração Traefik local (por nó).
 *
 * Serviços que rodam no nó atual geram configuração local com
 * resolução DNS interna do Docker, enquanto serviços remotos
 * usam a configuração de federação.
 */
export interface ILocalConfigGenerator {
    /**
     * Verifica se o serviço pode ter configuração local gerada.
     * @param service - Serviço descoberto
     * @returns `true` se o serviço roda no nó local
     */
    canGenerate(service: DiscoveredService): boolean;

    /**
     * Gera a configuração local Traefik para um serviço.
     * @param service - Serviço descoberto
     * @returns Configuração local ou `null` se o serviço não for local
     */
    generate(service: DiscoveredService): Promise<LocalConfigOutput | null>;
}
