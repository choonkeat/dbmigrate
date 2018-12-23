if test -n "`bash -version | grep 'version 3.2.'`"; then
    ESC="\x1B"
else
    ESC="\e"
fi
function assert() {
    desc=$1
    shift
    if $* 2>&1 > tests/output.log; then
        echo -e "${ESC}[42m${ESC}[97m[PASS]${ESC}[39m${ESC}[0m ${desc}"
        rm -f tests/output.log
    else
        echo -e "${ESC}[41m${ESC}[97m[FAIL]${ESC}[39m${ESC}[0m ${desc}"
        cat tests/output.log
        exit 1
    fi
}
function assert_fail() {
    desc=$1
    shift
    if $* 2>&1 > tests/output.log; then
        echo -e "${ESC}[41m${ESC}[97m[FAIL]${ESC}[39m${ESC}[0m ${desc}"
        cat tests/output.log
        exit 1
    else
        echo -e "${ESC}[42m${ESC}[97m[PASS]${ESC}[39m${ESC}[0m ${desc}"
        rm -f tests/output.log
    fi
}

function finish() {
  rm -rf ${DB_MIGRATIONS_DIR}
}
