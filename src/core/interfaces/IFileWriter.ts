/**
 * Interface para operações de escrita e leitura de arquivos YAML.
 */
export interface IFileWriter {
    /** Escreve dados em formato YAML em um arquivo */
    writeYaml(filePath: string, data: unknown): Promise<void>;

    /** Lê e faz parse de um arquivo YAML */
    readYaml<T>(filePath: string): Promise<T>;

    /** Remove um arquivo do sistema */
    deleteFile(filePath: string): Promise<void>;

    /** Garante que um diretório existe, criando-o se necessário */
    ensureDirectory(dirPath: string): Promise<void>;

    /** Lista os arquivos em um diretório */
    listFiles(dirPath: string): Promise<string[]>;

    /** Verifica se um arquivo ou diretório existe */
    exists(filePath: string): Promise<boolean>;
}
