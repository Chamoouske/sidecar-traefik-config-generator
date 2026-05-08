/**
 * LocalOrchestratorService - Orquestrador de configurações LOCAIS (node-specific).
 *
 * Responsável por gerenciar a geração de configurações específicas do nó
 * (roteamento local) com resolução DNS interna do Docker.
 * Pode ser executado em qualquer nó do Swarm.
 *
 * Usado em: GENERATION_MODE=local e GENERATION_MODE=all.
 *
 * Fluxo:
 * 1. Descobre serviços rodando no nó LOCAL
 * 2. Gera configurações de roteamento local para cada serviço
 * 3. Limpa arquivos obsoletos de serviços que não estão mais no nó
 */

import { ILogger } from '../core/interfaces/ILogger.js';
import { ILocalDiscoveryService } from '../core/interfaces/ILocalDiscoveryService.js';
import { ILocalConfigGenerator } from '../core/interfaces/ILocalConfigGenerator.js';
import { IFileWriter } from '../core/interfaces/IFileWriter.js';
import { ILocalOrchestrator } from '../core/interfaces/ILocalOrchestrator.js';
import { AppConfig } from '../types/config.js';
import { DiscoveredService } from '../types/docker.js';

export class LocalOrchestratorService implements ILocalOrchestrator {
    constructor(
        private readonly discovery: ILocalDiscoveryService,
        private readonly localGenerator: ILocalConfigGenerator,
        private readonly fileWriter: IFileWriter,
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Executa um ciclo completo de geração de configurações locais.
     *
     * Etapas:
     * 1. Descobre serviços rodando no nó atual
     * 2. Para cada serviço local, gera configuração de roteamento
     * 3. Remove arquivos de serviços que não estão mais no nó
     */
    async runGenerationCycle(): Promise<void> {
        this.logger.info('Iniciando ciclo de geração local');

        // 1. Discover services running on THIS node
        const services = await this.discovery.discoverLocalServices();
        this.logger.info('Serviços descobertos para geração local', {
            count: services.length,
        });

        if (services.length === 0) {
            this.logger.warn('Nenhum serviço local encontrado para geração local');
            return;
        }

        // 2. Track processed services
        const processedServices = new Set<string>();

        // 3. Generate local configs
        for (const service of services) {
            try {
                const localConfig = await this.localGenerator.generate(service);
                if (localConfig && localConfig.http.services) {
                    const localPath = `${this.config.directories.localGenerated}/${service.serviceName}.yaml`;
                    await this.fileWriter.writeYaml(localPath, localConfig);
                    this.logger.debug('Configuração local gerada', {
                        service: service.serviceName,
                        path: localPath,
                    });
                    processedServices.add(service.serviceName);
                }
            } catch (err) {
                this.logger.error('Falha ao gerar configuração local para serviço', {
                    service: service.serviceName,
                    error: (err as Error).message,
                });
            }
        }

        // 4. Clean up stale local files
        await this.cleanupStaleFiles(processedServices);

        this.logger.info('Ciclo de geração local concluído', {
            servicesProcessed: processedServices.size,
        });
    }

    /**
     * Remove arquivos de configuração local de serviços que não estão mais
     * rodando no nó atual.
     *
     * @param activeServices - Conjunto de nomes de serviços ativos no nó
     */
    private async cleanupStaleFiles(activeServices: Set<string>): Promise<void> {
        try {
            const files = await this.fileWriter.listFiles(this.config.directories.localGenerated);
            for (const filePath of files) {
                const fileName = filePath.split(/[/\\]/).pop()?.replace('.yaml', '') ?? '';
                if (!activeServices.has(fileName)) {
                    await this.fileWriter.deleteFile(filePath);
                    this.logger.info('Arquivo obsoleto removido (local)', {
                        filePath,
                    });
                }
            }
        } catch (err) {
            const nodeError = err as NodeJS.ErrnoException;
            if (nodeError.code !== 'ENOENT') {
                this.logger.warn('Falha ao limpar diretório local', {
                    directory: this.config.directories.localGenerated,
                    error: (err as Error).message,
                });
            }
        }
    }
}
