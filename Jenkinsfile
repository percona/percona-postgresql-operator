GKERegion='us-central1-c'

void CreateCluster(String CLUSTER_PREFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_PREFIX}
            source $HOME/google-cloud-sdk/path.bash.inc
            gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
            gcloud config set project $GCP_PROJECT
            gcloud container clusters create --zone=${GKERegion} $CLUSTER_NAME-${CLUSTER_PREFIX} --cluster-version=1.20 --machine-type=n1-standard-4 --preemptible --num-nodes=3 --network=jenkins-pg-vpc --subnetwork=jenkins-pg-${CLUSTER_PREFIX} --no-enable-autoupgrade
            kubectl create clusterrolebinding cluster-admin-binding --clusterrole cluster-admin --user jenkins@"$GCP_PROJECT".iam.gserviceaccount.com
        """
   }
}
void ShutdownCluster(String CLUSTER_PREFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_PREFIX}
            source $HOME/google-cloud-sdk/path.bash.inc
            gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
            gcloud config set project $GCP_PROJECT
            gcloud container clusters delete --zone $GKERegion $CLUSTER_NAME-${CLUSTER_PREFIX}
        """
   }
}
void pushArtifactFile(String FILE_NAME) {
    echo "Push $FILE_NAME file to S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            touch ${FILE_NAME}
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 ls \$S3_PATH/${FILE_NAME} || :
            aws s3 cp --quiet ${FILE_NAME} \$S3_PATH/${FILE_NAME} || :
        """
    }
}

void popArtifactFile(String FILE_NAME) {
    echo "Try to get $FILE_NAME file from S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 cp --quiet \$S3_PATH/${FILE_NAME} ${FILE_NAME} || :
        """
    }
}

TestsReport = '| Test name  | Status |\r\n| ------------- | ------------- |'
testsReportMap  = [:]
testsResultsMap = [:]

void makeReport() {
    for ( test in testsReportMap ) {
        TestsReport = TestsReport + "\r\n| ${test.key} | ${test.value} |"
    }
}

void setTestsresults() {
    testsResultsMap.each { file ->
        pushArtifactFile("${file.key}")
    }
}

void runTest(String TEST_NAME, String CLUSTER_PREFIX) {
    def retryCount = 0
    waitUntil {
        try {
            echo "The $TEST_NAME test was started!"
            testsReportMap[TEST_NAME] = 'failed'
            popArtifactFile("${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME")

            timeout(time: 90, unit: 'MINUTES') {
                sh """
                    if [ -f "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME" ]; then
                        echo Skip $TEST_NAME test
                    else
                        export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_PREFIX}
                        source $HOME/google-cloud-sdk/path.bash.inc
                        ./e2e-tests/$TEST_NAME/run
                    fi
                """
            }
            testsReportMap[TEST_NAME] = 'passed'
            testsResultsMap["${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME"] = 'passed'
            return true
        }
        catch (exc) {
            if (retryCount >= 2) {
                currentBuild.result = 'FAILURE'
                return true
            }
            retryCount++
            return false
        }
    }

    echo "The $TEST_NAME test was finished!"
}


def skipBranchBulds = true
if ( env.CHANGE_URL ) {
    skipBranchBulds = false
}
def FILES_CHANGED = null
def IMAGE_EXISTS = null
def PREVIOUS_IMAGE_EXISTS = null

pipeline {
    environment {
        CLEAN_NAMESPACE = 1
        CLOUDSDK_CORE_DISABLE_PROMPTS = 1
        GIT_SHORT_COMMIT = sh(script: 'git describe --always --dirty', , returnStdout: true).trim()
        GIT_PREV_SHORT_COMMIT = sh(script: 'git describe --always HEAD~1', , returnStdout: true).trim()
        VERSION = "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}"
        CLUSTER_NAME = sh(script: "echo jenkins-pgo-${GIT_SHORT_COMMIT} | tr '[:upper:]' '[:lower:]'", , returnStdout: true).trim()
        PGO_K8S_NAME = "${env.CLUSTER_NAME}-upstream"
        AUTHOR_NAME  = sh(script: "echo ${CHANGE_AUTHOR_EMAIL} | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
        ECR = "119175775298.dkr.ecr.us-east-1.amazonaws.com"
    }
    agent {
        label 'docker'
    }
    stages {
        stage('Prepare') {
            when {
                expression {
                    !skipBranchBulds
                }
            }
            steps {
                script {
                    if ( AUTHOR_NAME == 'null' )  {
                        AUTHOR_NAME = sh(script: "git show -s --pretty=%ae | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
                    }
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                }
                sh '''
                    if [ ! -d $HOME/google-cloud-sdk/bin ]; then
                        rm -rf $HOME/google-cloud-sdk
                        curl https://sdk.cloud.google.com | bash
                    fi

                    source $HOME/google-cloud-sdk/path.bash.inc
                    gcloud components install alpha
                    gcloud components install kubectl

                    curl -fsSL https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
                    curl -s -L https://github.com/openshift/origin/releases/download/v3.11.0/openshift-origin-client-tools-v3.11.0-0cbc58b-linux-64bit.tar.gz \
                        | sudo tar -C /usr/local/bin --strip-components 1 --wildcards -zxvpf - '*/oc'

                    curl -s -L https://github.com/mitchellh/golicense/releases/latest/download/golicense_0.2.0_linux_x86_64.tar.gz \
                        | sudo tar -C /usr/local/bin --wildcards -zxvpf -

                    sudo sh -c "curl -s -L https://github.com/mikefarah/yq/releases/download/3.3.2/yq_linux_amd64 > /usr/local/bin/yq"
                    sudo chmod +x /usr/local/bin/yq

                    sudo yum install -y jq
                '''
                script {
                    FILES_CHANGED = sh(script: "git diff --name-only HEAD HEAD~1 | grep -Ev 'e2e-tests|Jenkinsfile' | head -1", , returnStdout: true).trim() ?: null
                    IMAGE_EXISTS = sh(script: 'curl https://registry.hub.docker.com/v1/repositories/perconalab/percona-postgresql-operator/tags | jq -r \'.[].name\' | grep $VERSION | head -1', , returnStdout: true).trim() ?: null
                    PREVIOUS_IMAGE_EXISTS = sh(script: 'curl https://registry.hub.docker.com/v1/repositories/perconalab/percona-postgresql-operator/tags | jq -r \'.[].name\' | grep $GIT_BRANCH-$GIT_PREV_SHORT_COMMIT | head -1', , returnStdout: true).trim() ?: null
                }
                withCredentials([file(credentialsId: 'cloud-secret-file', variable: 'CLOUD_SECRET_FILE'), file(credentialsId: 'cloud-minio-secret-file', variable: 'CLOUD_MINIO_SECRET_FILE')]) {
                    sh '''
                        cp $CLOUD_SECRET_FILE ./e2e-tests/conf/cloud-secret.yml
                        cp $CLOUD_MINIO_SECRET_FILE ./e2e-tests/conf/cloud-secret-minio-gw.yml
                    '''
                }
            }
        }
        stage('Retag previous commit images if possible'){
            when {
                allOf {
                    expression { !skipBranchBulds }
                    allOf {
                        expression { FILES_CHANGED == null }
                        expression { IMAGE_EXISTS == null }
                        expression { PREVIOUS_IMAGE_EXISTS != null }
                    }
                }
            }
            steps {
                withCredentials([usernamePassword(credentialsId: 'hub.docker.com', passwordVariable: 'PASS', usernameVariable: 'USER'), [$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                    sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                            sg docker -c "
                                docker login -u '${USER}' -p '${PASS}'
                                aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR
                                export IMAGE=\$URI_BASE

                                for app in "pgo-apiserver" "pgo-event" "pgo-rmdata" "pgo-scheduler" "postgres-operator" "pgo-deployer"; do
                                    docker pull perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app}
                                    docker tag perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app} \\${IMAGE}-\\${app}
                                    docker tag perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app} $ECR/\\${IMAGE}-\\${app}
                                    docker push \\${IMAGE}-\\${app}
                                    docker push $ECR/\\${IMAGE}-\\${app}
                                done
                                docker logout
                            "
                        sudo rm -rf ./build
                    '''
                }
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('Build docker image') {
            when {
                allOf {
                    expression { !skipBranchBulds }
                    anyOf {
                        allOf {
                            expression { FILES_CHANGED != null }
                            expression { IMAGE_EXISTS == null }
                        }
                        allOf {
                            expression { FILES_CHANGED == null }
                            expression { IMAGE_EXISTS == null }
                            expression { PREVIOUS_IMAGE_EXISTS == null }
                        }
                    }
                }
            }
            steps {
                withCredentials([usernamePassword(credentialsId: 'hub.docker.com', passwordVariable: 'PASS', usernameVariable: 'USER'),[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                    sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                            sg docker -c "
                                docker login -u '${USER}' -p '${PASS}'
                                aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR
                                export IMAGE=\$URI_BASE
                                ./e2e-tests/build
                                docker logout
                            "
                        sudo rm -rf ./build
                    '''
                }
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('Save dummy URI_BASE') {
            when {
                allOf {
                    expression { !skipBranchBulds }
                    expression { IMAGE_EXISTS != null }
                }
            }
            steps {
                sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                        sudo rm -rf ./build
                    '''
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('GoLicenseDetector test') {
            when {
                expression {
                    !skipBranchBulds
                }
            }
            steps {
                sh """
                    mkdir -p $WORKSPACE/src/github.com/percona
                    ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                    sg docker -c "
                        docker run \
                            --rm \
                            -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                            -w /go/src/github.com/percona/percona-postgresql-operator \
                            -e GO111MODULE=on \
                            golang:1.16 sh -c '
                                go get github.com/google/go-licenses;
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/apiserver \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > apiserver-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/pgo-rmdata \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > pgo-rmdata-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > pgo-scheduler-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/postgres-operator \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > postgres-operator-licenses-new || :
                            '
                    "
                    diff -u e2e-tests/license/compare/apiserver-licenses apiserver-licenses-new
                    diff -u e2e-tests/license/compare/pgo-rmdata-licenses pgo-rmdata-licenses-new
                    diff -u e2e-tests/license/compare/pgo-scheduler-licenses pgo-scheduler-licenses-new
                    diff -u e2e-tests/license/compare/postgres-operator-licenses postgres-operator-licenses-new
                 """
            }
        }
        stage('GoLicense test') {
            when {
                expression {
                    !skipBranchBulds
                }
            }
            steps {
                sh '''
                    mkdir -p $WORKSPACE/src/github.com/percona
                    ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                    sg docker -c "
                        docker run \
                            --rm \
                            -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                            -w /go/src/github.com/percona/percona-postgresql-operator \
                            -e GO111MODULE=on \
                            golang:1.16 sh -c 'go build -v -o apiserver github.com/percona/percona-postgresql-operator/cmd/apiserver;
                                               go build -v -o pgo-rmdata github.com/percona/percona-postgresql-operator/cmd/pgo-rmdata;
                                               go build -v -o pgo-scheduler github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler;
                                               go build -v -o postgres-operator github.com/percona/percona-postgresql-operator/cmd/postgres-operator '
                    "
                '''

                withCredentials([string(credentialsId: 'GITHUB_API_TOKEN', variable: 'GITHUB_TOKEN')]) {
                    sh """
                        golicense -plain ./apiserver \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > apiserver-golicense-new || true
                        golicense -plain ./pgo-rmdata \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > pgo-rmdata-golicense-new || true
                        golicense -plain ./pgo-scheduler \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > pgo-scheduler-golicense-new || true
                        golicense -plain ./postgres-operator \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > postgres-operator-golicense-new || true

                        diff -u e2e-tests/license/compare/apiserver-golicense apiserver-golicense-new
                        diff -u e2e-tests/license/compare/pgo-rmdata-golicense pgo-rmdata-golicense-new
                        diff -u e2e-tests/license/compare/pgo-scheduler-golicense pgo-scheduler-golicense-new
                        diff -u e2e-tests/license/compare/postgres-operator-golicense postgres-operator-golicense-new
                    """
                }
            }
        }
        stage('Run tests for operator') {
            when {
                expression {
                    !skipBranchBulds
                }
            }
            options {
                timeout(time: 3, unit: 'HOURS')
            }
            parallel {
                stage('E2E Basic tests') {
                    steps {
                        CreateCluster('sandbox')
                        withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                            runTest('init-deploy', 'sandbox')
                        }
                        runTest('scaling', 'sandbox')
                        runTest('recreate', 'sandbox')
                        runTest('affinity', 'sandbox')
                        runTest('monitoring', 'sandbox')
                        runTest('self-healing', 'sandbox')
                        runTest('clone-cluster', 'sandbox')
                        ShutdownCluster('sandbox')
                    }
                }
                stage('E2E Backups') {
                    steps {
                        CreateCluster('backups')
                        runTest('demand-backup', 'backups')
                        ShutdownCluster('backups')
                    }
                }
                stage('E2E Data migration') {
                    steps {
                        CreateCluster('upstream')
                        CreateCluster('migration')
                        runTest('data-migration-gcs', 'migration')
                        ShutdownCluster('migration')
                        ShutdownCluster('upstream')
                    }
                }
            }
        }
    }
    post {
        always {
            script {
                setTestsresults()
                if (currentBuild.result != null && currentBuild.result != 'SUCCESS') {
                    slackSend channel: '#cloud-dev-ci', color: '#FF0000', message: "[${JOB_NAME}]: build ${currentBuild.result}, ${BUILD_URL} owner: @${AUTHOR_NAME}"
                }
                if (env.CHANGE_URL) {
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                    makeReport()
                    unstash 'URI_BASE'
                    def URI_BASE = sh(returnStdout: true, script: "cat results/docker/URI_BASE").trim()
                    TestsReport = TestsReport + "\r\n\r\ncommit: ${env.CHANGE_URL}/commits/${env.GIT_COMMIT}\r\nimage: `${URI_BASE}-pgo-apiserver`\r\n\r\nimage: `${URI_BASE}-pgo-event`\r\n\r\nimage: `${URI_BASE}-pgo-rmdata`\r\n\r\nimage: `${URI_BASE}-pgo-scheduler`\r\n\r\nimage: `${URI_BASE}-postgres-operator`\r\n\r\nimage: `${URI_BASE}-pgo-deployer`\r\n"
                    pullRequest.comment(TestsReport)

                    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
                        sh """
                            source $HOME/google-cloud-sdk/path.bash.inc
                            gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
                            gcloud config set project $GCP_PROJECT
                            gcloud container clusters list --format='csv[no-heading](name)' --filter $CLUSTER_NAME | xargs gcloud container clusters delete --zone $GKERegion --quiet || true
                            sudo docker rmi -f \$(sudo docker images -q) || true
                            sudo rm -rf $HOME/google-cloud-sdk
                        """
                    }
                }
            }
            sh '''
                sudo docker rmi -f \$(sudo docker images -q) || true
                sudo rm -rf ./*
                sudo rm -rf $HOME/google-cloud-sdk
            '''
            deleteDir()
        }
    }
}
