/**
 * FileWriterService - Operações de escrita e leitura de arquivos YAML.
 *
 * Implementa a interface IFileWriter com suporte a:
 * - Atomic writes (arquivo temporário + rename) para evitar corrupção
 * - Verificação de mudanças antes de escrever (evita writes desnecessários)
 * - Criação automática de diretórios
 * - Logging structured em cada operação
 */

import { IFileWriter } from '../core/interfaces/IFileWriter.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { AppConfig } from '../types/config.js';
import { FileWriteError } from '../types/errors.js';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import * as yaml from 'js-yaml';

export class FileWriterService implements IFileWriter {
    constructor(
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Escreve dados em formato YAML em um arquivo usando atomic write.
     *
     * O processo de escrita atômica funciona em três etapas:
     * 1. Serializa os dados para YAML
     * 2. Se o arquivo já existe, compara o conteúdo atual com o novo
     * 3. Se diferente ou não existe, escreve para um arquivo `.tmp` e renomeia
     *
     * @param filePath - Caminho absoluto ou relativo do arquivo de destino
     * @param data     - Dados a serem serializados em YAML
     * @throws {FileWriteError} Se houver falha na escrita
     */
    async writeYaml(filePath: string, data: unknown): Promise<void> {
        const targetPath = path.resolve(filePath);
        const dir = path.dirname(targetPath);

        try {
            // Garante que o diretório pai existe
            await this.ensureDirectory(dir);

            // Serializa os dados para YAML
            const content = yaml.dump(data, {
                indent: 2,
                lineWidth: 120,
                noRefs: true,
                quotingType: "'",
                forceQuotes: false,
            });

            // Verifica se o conteúdo já é o mesmo para evitar writes desnecessários
            const existingContent = await this.readExistingContent(targetPath);
            if (existingContent !== null && existingContent === content) {
                this.logger.debug('Conteúdo inalterado, pulando escrita', {
                    filePath: targetPath,
                });
                return;
            }

            // Atomic write: escreve para arquivo temporário e renomeia
            const tmpFile = path.join(
                dir,
                `.${path.basename(targetPath)}.tmp.${process.pid}`,
            );

            await fs.writeFile(tmpFile, content, 'utf-8');
            await fs.rename(tmpFile, targetPath);

            this.logger.info('Arquivo YAML escrito com sucesso', {
                filePath: targetPath,
            });
        } catch (error) {
            throw new FileWriteError(
                `Falha ao escrever arquivo YAML: ${(error as Error).message}`,
                targetPath,
                error instanceof Error ? error : undefined,
            );
        }
    }

    /**
     * Lê e faz parse de um arquivo YAML.
     *
     * @param filePath - Caminho do arquivo a ser lido
     * @returns Dados parseados do YAML
     * @throws {FileWriteError} Se o arquivo não existir ou houver erro de parse
     */
    async readYaml<T>(filePath: string): Promise<T> {
        const targetPath = path.resolve(filePath);

        try {
            const content = await fs.readFile(targetPath, 'utf-8');
            const data = yaml.load(content) as T;

            this.logger.debug('Arquivo YAML lido com sucesso', {
                filePath: targetPath,
            });

            return data;
        } catch (error) {
            throw new FileWriteError(
                `Falha ao ler arquivo YAML: ${(error as Error).message}`,
                targetPath,
                error instanceof Error ? error : undefined,
            );
        }
    }

    /**
     * Remove um arquivo do sistema.
     *
     * Não lança erro se o arquivo não existir (idempotente).
     *
     * @param filePath - Caminho do arquivo a ser removido
     */
    async deleteFile(filePath: string): Promise<void> {
        const targetPath = path.resolve(filePath);

        try {
            await fs.unlink(targetPath);
            this.logger.info('Arquivo removido com sucesso', {
                filePath: targetPath,
            });
        } catch (error) {
            const nodeError = error as NodeJS.ErrnoException;
            // Se o arquivo não existe, considera sucesso (idempotente)
            if (nodeError.code === 'ENOENT') {
                this.logger.debug('Arquivo não existe, ignorando remoção', {
                    filePath: targetPath,
                });
                return;
            }

            throw new FileWriteError(
                `Falha ao deletar arquivo: ${(error as Error).message}`,
                targetPath,
                error instanceof Error ? error : undefined,
            );
        }
    }

    /**
     * Garante que um diretório existe, criando-o recursivamente se necessário.
     *
     * Não lança erro se o diretório já existir (idempotente).
     *
     * @param dirPath - Caminho do diretório a ser verificado/criado
     * @throws {FileWriteError} Se houver falha na criação
     */
    async ensureDirectory(dirPath: string): Promise<void> {
        const targetPath = path.resolve(dirPath);

        try {
            await fs.mkdir(targetPath, { recursive: true });
            this.logger.debug('Diretório garantido', {
                dirPath: targetPath,
            });
        } catch (error) {
            throw new FileWriteError(
                `Falha ao criar diretório: ${(error as Error).message}`,
                targetPath,
                error instanceof Error ? error : undefined,
            );
        }
    }

    /**
     * Lista os arquivos em um diretório, retornando paths completos.
     *
     * @param dirPath - Caminho do diretório a ser listado
     * @returns Lista de caminhos completos dos arquivos
     * @throws {FileWriteError} Se o diretório não existir ou houver erro
     */
    async listFiles(dirPath: string): Promise<string[]> {
        const targetPath = path.resolve(dirPath);

        try {
            const entries = await fs.readdir(targetPath, {
                withFileTypes: true,
            });

            const files = entries
                .filter((entry) => entry.isFile())
                .map((entry) => path.join(targetPath, entry.name));

            this.logger.debug('Arquivos listados', {
                dirPath: targetPath,
                count: files.length,
            });

            return files;
        } catch (error) {
            throw new FileWriteError(
                `Falha ao listar arquivos: ${(error as Error).message}`,
                targetPath,
                error instanceof Error ? error : undefined,
            );
        }
    }

    /**
     * Verifica se um arquivo ou diretório existe no sistema.
     *
     * @param filePath - Caminho a ser verificado
     * @returns `true` se existir, `false` caso contrário
     */
    async exists(filePath: string): Promise<boolean> {
        const targetPath = path.resolve(filePath);

        try {
            await fs.access(targetPath);
            return true;
        } catch {
            return false;
        }
    }

    /**
     * Lê o conteúdo existente de um arquivo, se ele existir.
     *
     * @param filePath - Caminho do arquivo
     * @returns Conteúdo do arquivo ou `null` se não existir
     */
    private async readExistingContent(filePath: string): Promise<string | null> {
        try {
            return await fs.readFile(filePath, 'utf-8');
        } catch {
            return null;
        }
    }
}
