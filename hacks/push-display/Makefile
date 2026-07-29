HOOK_SRC    = src/push_hook.c
HOOK_OUT    = push_hook.so
SPLASH_H    = src/splash_data.h
GEN_SPLASH  = gen_splash/gen_splash
DOCKER      = docker run --rm --platform linux/amd64 \
                -v "$(CURDIR)":/work -w /work \
                gcc:12-bullseye

.PHONY: all splash clean

all: $(HOOK_OUT)

# Build push_hook.so (requires splash_data.h)
$(HOOK_OUT): $(HOOK_SRC) $(SPLASH_H)
	$(DOCKER) gcc -shared -fPIC -o $@ $(HOOK_SRC) -ldl -lpthread -O2 \
	  -Wall -Wno-unused-function \
	  -D_GNU_SOURCE
	@echo "Built: $@  ($$(file $(HOOK_OUT) | grep -o 'ELF.*'))"

# Generate splash_data.h from the gen_splash Go tool
splash: $(SPLASH_H)

$(SPLASH_H): $(GEN_SPLASH)
	$(GEN_SPLASH)

# Build the gen_splash tool (runs on the build machine, not cross-compiled)
$(GEN_SPLASH): gen_splash/main.go gen_splash/go.mod gen_splash/go.sum
	cd gen_splash && go build -o gen_splash .

clean:
	rm -f $(HOOK_OUT) $(SPLASH_H) $(GEN_SPLASH)
