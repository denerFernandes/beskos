# Imagem mínima para a app `mail` do CapRover: só existe para o nome mail.beskos.com ter
# certificado válido e responder à descoberta dos clientes de correio. Ver nginx.conf.
FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
