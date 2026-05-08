/**
 * GlobalOrchestratorService - Orquestrador de configurações GLOBAIS.
 *
 * Responsável por gerenciar a geração de configurações compartilhadas
 * (federação + middlewares) que são distribuídas para todos os nós.
 * Deve ser executado apenas em nós managers do Swarm.
 *
 * Usado em: GENERATION_MODE=global e GENERATION_MODE=all.
 *
 * Fluxo:
 * 1. Descobre todos os serviços federados no Swarm
 * 2. Gera configs de federação e middlewares para cada serviço
 * 3. Limpa arquivos obsoletos de serviços que não existem mais
 */

import { ILogger } from '../core/interfaces/ILogger.js';
import { ISwarmDiscovery } from '../core/interfaces/ISwarmDiscovery.js';
import { IFederationStrategy } from '../core/interfaces/IFederationStrategy.js';
import { IMiddlewareGenerator } from '../core/interfaces/IMiddlewareGenerator.js';
import { IFileWriter } from '../core/interfaces/IFileWriter.js';
import { IGlobalOrchestrator } from '../core/interfaces/IGlobalOrchestrator.js';
import { AppConfig } from '../types/config.js';
import { DiscoveredService } from '../types/docker.js';

export class GlobalOrchestratorService implements IGlobalOrchestrator {
    constructor(
        private readonly discovery: ISwarmDiscovery,
        private readonly federationGenerator: IFederationStrategy,
        private readonly middlewareGenerator: IMiddlewareGenerator,
        private readonly fileWriter: IFileWriter,
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Executa um ciclo completo de geração de configurações globais.
     *
     * Etapas:
     * 1. Descobre todos os serviços federados no Swarm
     * 2. Para cada serviço, gera configuração de federação + middlewares
     * 3. Remove arquivos de serviços que não existem mais
     */
    async runGenerationCycle(): Promise<void> {
        this.logger.info('Iniciando ciclo de geração global');

        // 1. Descobre todos os serviços federados no Swarm
        const services = await this.discovery.discoverAllServices();
        this.logger.info('Serviços descobertos para geração global', {
            count: services.length,
        });

        if (services.length === 0) {
            this.logger.warn('Nenhum serviço federado encontrado para geração global');
            return;
        }

        // 2. Track which services were processed
        const processedServices = new Set<string>();

        // 3. Generate configs for each service
        for (const service of services) {
            try {
                await this.generateForService(service);
                processedServices.add(service.serviceName);
            } catch (err) {
                this.logger.error('Falha ao gerar configuração global para serviço', {
                    service: service.serviceName,
                    error: (err as Error).message,
                });
            }
        }

        // 4. Clean up stale files
        await this.cleanupStaleFiles(processedServices);

        this.logger.info('Ciclo de geração global concluído', {
            servicesProcessed: processedServices.size,
        });
    }

    /**
     * Gera as configurações de federação e middlewares para um serviço.
     *
     * @param service - Serviço descoberto a ser processado
     */
    private async generateForService(service: DiscoveredService): Promise<void> {
        const serviceLogger = this.logger.child({ serviceName: service.serviceName });

        // Generate federation config
        if (this.federationGenerator.canHandle(service)) {
            const federationConfig = await this.federationGenerator.generate(service);
            const federationPath = `${this.config.directories.federation}/${service.serviceName}.yaml`;
            await this.fileWriter.writeYaml(federationPath, federationConfig);
            serviceLogger.debug('Configuração de federação gerada', {
                path: federationPath,
            });
        }

        // Generate middleware config
        const middlewareConfig = await this.middlewareGenerator.generate(service);
        if (middlewareConfig && middlewareConfig.http.middlewares) {
            const middlewarePath = `${this.config.directories.middlewares}/${service.serviceName}.yaml`;
            await this.fileWriter.writeYaml(middlewarePath, middlewareConfig);
            serviceLogger.debug('Configuração de middlewares gerada', {
                path: middlewarePath,
            });
        }
    }

    /**
     * Remove arquivos de configuração de serviços que não existem mais.
     *
     * Varre os diretórios de federação e middlewares e remove arquivos
     * cujo nome (sem extensão) não corresponde a nenhum serviço ativo.
     *
     * @param activeServices - Conjunto de nomes de serviços ativos
     */
    private async cleanupStaleFiles(activeServices: Set<string>): Promise<void> {
        const dirs = [
            this.config.directories.federation,
            this.config.directories.middlewares,
        ];

        for (const dir of dirs) {
            try {
                const files = await this.fileWriter.listFiles(dir);
                for (const filePath of files) {
                    // Extract service name from filename: /path/to/service-name.yaml → service-name
                    const fileName = filePath.split(/[/\\]/).pop()?.replace('.yaml', '') ?? '';
                    if (!activeServices.has(fileName)) {
                        await this.fileWriter.deleteFile(filePath);
                        this.logger.info('Arquivo obsoleto removido (global)', {
                            filePath,
                        });
                    }
                }
            } catch (err) {
                const nodeError = err as NodeJS.ErrnoException;
                if (nodeError.code !== 'ENOENT') {
                    this.logger.warn('Falha ao limpar diretório global', {
                        directory: dir,
                        error: (err as Error).message,
                    });
                }
            }
        }
    }
}
