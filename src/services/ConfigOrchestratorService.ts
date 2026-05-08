/**
 * ConfigOrchestratorService - Orquestrador de geração de configuração Traefik.
 *
 * Coordena o ciclo completo de geração de configuração:
 * 1. Descoberta de serviços no Swarm
 * 2. Geração de configuração de federação
 * 3. Geração de middlewares
 * 4. Geração de configuração local (se aplicável)
 * 5. Escrita dos arquivos YAML nos diretórios apropriados
 * 6. Limpeza de arquivos obsoletos de serviços que não existem mais
 */

import { ILogger } from '../core/interfaces/ILogger.js';
import { ISwarmDiscovery } from '../core/interfaces/ISwarmDiscovery.js';
import { IFileWriter } from '../core/interfaces/IFileWriter.js';
import { IFederationStrategy } from '../core/interfaces/IFederationStrategy.js';
import { IMiddlewareGenerator } from '../core/interfaces/IMiddlewareGenerator.js';
import { LocalConfigGeneratorService } from '../generators/LocalConfigGeneratorService.js';
import { AppConfig } from '../types/config.js';
import { DiscoveredService } from '../types/docker.js';

export class ConfigOrchestratorService {
    constructor(
        private readonly discovery: ISwarmDiscovery,
        private readonly federationGenerator: IFederationStrategy,
        private readonly localGenerator: LocalConfigGeneratorService,
        private readonly middlewareGenerator: IMiddlewareGenerator,
        private readonly fileWriter: IFileWriter,
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Executa um ciclo completo de geração de configuração.
     *
     * Fluxo:
     * 1. Descobre todos os serviços federados no Swarm
     * 2. Para cada serviço, gera e salva as configurações
     * 3. Limpa arquivos de serviços que não existem mais
     * 4. Loga resumo da execução
     */
    async runGenerationCycle(): Promise<void> {
        this.logger.info('Iniciando ciclo de geração de configuração');

        try {
            // 1. Descobre todos os serviços
            const services = await this.discovery.discoverAllServices();
            this.logger.info('Serviços descobertos', {
                count: services.length,
            });

            if (services.length === 0) {
                this.logger.warn('Nenhum serviço federado encontrado');
                return;
            }

            // 2. Processa cada serviço
            let federationCount = 0;
            let middlewareCount = 0;
            let localCount = 0;

            for (const service of services) {
                const result = await this.generateForService(service);

                if (result.federationGenerated) federationCount++;
                if (result.middlewareGenerated) middlewareCount++;
                if (result.localGenerated) localCount++;
            }

            // 3. Limpa arquivos obsoletos
            const currentServiceNames = new Set(
                services.map((s) => s.serviceName),
            );
            await this.cleanupStaleFiles(currentServiceNames);

            // 4. Log de resumo
            this.logger.info('Ciclo de geração concluído', {
                totalServices: services.length,
                federationConfigs: federationCount,
                middlewareConfigs: middlewareCount,
                localConfigs: localCount,
            });
        } catch (error) {
            this.logger.error('Erro no ciclo de geração', {
                error: (error as Error).message,
            });
        }
    }

    /**
     * Gera e salva as configurações para um único serviço.
     *
     * Etapas:
     * 1. Gera configuração de federação e salva no diretório federation/
     * 2. Gera middlewares e salva no diretório middlewares/
     * 3. Se o serviço for local, gera configuração local e salva
     * 4. Se o serviço não for mais local mas tinha config local, remove
     *
     * @param service - Serviço descoberto a ser processado
     * @returns Objeto indicando quais tipos de configuração foram gerados
     */
    private async generateForService(service: DiscoveredService): Promise<{
        federationGenerated: boolean;
        middlewareGenerated: boolean;
        localGenerated: boolean;
    }> {
        const serviceName = service.serviceName;
        const serviceLogger = this.logger.child({ serviceName });
        const result = {
            federationGenerated: false,
            middlewareGenerated: false,
            localGenerated: false,
        };

        try {
            // 1. Configuração de federação
            if (this.federationGenerator.canHandle(service)) {
                const federationConfig = await this.federationGenerator.generate(
                    service,
                );

                if (federationConfig && federationConfig.http.services) {
                    const fedPath = `${this.config.directories.federation}/${serviceName}.yaml`;
                    await this.fileWriter.writeYaml(fedPath, federationConfig);
                    result.federationGenerated = true;
                    serviceLogger.info('Configuração de federação salva', {
                        path: fedPath,
                    });
                }
            }

            // 2. Middlewares
            const middlewareConfig = await this.middlewareGenerator.generate(
                service,
            );

            if (middlewareConfig && middlewareConfig.http.middlewares) {
                const mwPath = `${this.config.directories.middlewares}/${serviceName}.yaml`;
                await this.fileWriter.writeYaml(mwPath, middlewareConfig);
                result.middlewareGenerated = true;
                serviceLogger.info('Middlewares salvos', {
                    path: mwPath,
                });
            }

            // 3. Configuração local
            const isLocal = this.localGenerator.canGenerate(service);
            const localPath = `${this.config.directories.localGenerated}/${serviceName}.yaml`;

            if (isLocal) {
                const localConfig = await this.localGenerator.generate(service);

                if (localConfig && localConfig.http.services) {
                    await this.fileWriter.writeYaml(localPath, localConfig);
                    result.localGenerated = true;
                    serviceLogger.info('Configuração local salva', {
                        path: localPath,
                    });
                }
            } else {
                // Remove config local obsoleta se serviço não é mais local
                const localExists = await this.fileWriter.exists(localPath);
                if (localExists) {
                    await this.fileWriter.deleteFile(localPath);
                    serviceLogger.info(
                        'Configuração local removida (serviço não é mais local)',
                        { path: localPath },
                    );
                }
            }
        } catch (error) {
            serviceLogger.error('Erro ao gerar configuração', {
                error: (error as Error).message,
            });
        }

        return result;
    }

    /**
     * Limpa arquivos de configuração de serviços que não existem mais.
     *
     * Varre os diretórios de saída e remove arquivos cujo nome
     * (sem extensão) não corresponde a nenhum serviço ativo.
     *
     * @param currentServices - Conjunto de nomes de serviços ativos
     */
    private async cleanupStaleFiles(
        currentServices: Set<string>,
    ): Promise<void> {
        const directories = [
            {
                path: this.config.directories.federation,
                label: 'federação',
            },
            {
                path: this.config.directories.middlewares,
                label: 'middlewares',
            },
            {
                path: this.config.directories.localGenerated,
                label: 'local',
            },
        ];

        let removedCount = 0;

        for (const dir of directories) {
            try {
                const files = await this.fileWriter.listFiles(dir.path);

                for (const filePath of files) {
                    // Extrai o nome do serviço do nome do arquivo (ex: "meu-servico.yaml")
                    const fileName = filePath.split(/[/\\]/).pop() || '';
                    const serviceName = fileName.replace(/\.yaml$/, '');

                    // Se o serviço não existe mais, remove o arquivo
                    if (!currentServices.has(serviceName)) {
                        await this.fileWriter.deleteFile(filePath);
                        removedCount++;
                        this.logger.debug(
                            'Arquivo obsoleto removido',
                            {
                                filePath,
                                type: dir.label,
                                serviceName,
                            },
                        );
                    }
                }
            } catch (error) {
                // Ignora erros de diretório inexistente
                const nodeError = error as NodeJS.ErrnoException;
                if (nodeError.code !== 'ENOENT') {
                    this.logger.warn(
                        'Erro ao limpar arquivos obsoletos',
                        {
                            directory: dir.path,
                            error: (error as Error).message,
                        },
                    );
                }
            }
        }

        if (removedCount > 0) {
            this.logger.info('Arquivos obsoletos removidos', {
                count: removedCount,
            });
        }
    }
}
