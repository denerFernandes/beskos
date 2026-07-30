# Serviço de backup: pg_dump + rclone, três vezes por dia.
#
# Imagem própria e pequena em vez de meter o pg_dump na imagem da API: são responsabilidades
# diferentes, e uma falha aqui não deve poder derrubar a aplicação (nem o contrário).
FROM alpine:3.20

# postgresql16-client: a versão do cliente TEM de ser >= à do servidor (produção é pg16), senão o
# pg_dump recusa-se a exportar.
#
# age: cifra as cópias antes de saírem daqui. Fica na mesma imagem que o restore.sh usa, para que
# a ferramenta que decifra seja exatamente a que cifrou — descobrir uma incompatibilidade de versões
# no dia do restauro é o pesadelo que este serviço existe para evitar.
RUN apk add --no-cache bash postgresql16-client rclone curl tzdata age

COPY backup.sh /usr/local/bin/backup.sh
COPY restore.sh /usr/local/bin/restore.sh
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/backup.sh /usr/local/bin/restore.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
