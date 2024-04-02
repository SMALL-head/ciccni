FROM 1.22-alpine as ciccni-build

COPY . /ciccni

WORKDIR /ciccni

RUN make bin

FROM ubuntu:18.04

USER root

# 脚本拷贝至/usr/local/bin下就可以直接调用了
COPY --from=ciccni-build /ciccni/build/images/scripts/* /usr/local/bin/
COPY --from=ciccni-build /ciccni/bin/* /usr/local/bin/